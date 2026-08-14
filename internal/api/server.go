// Package api 提供 HTTP API 和 Web 页面服务。
package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"ip-allowlist/internal/auth"
	"ip-allowlist/internal/iptables"
	"ip-allowlist/internal/store"
	"ip-allowlist/internal/sysinfo"
)

// Server HTTP 服务。
type Server struct {
	router    *gin.Engine
	store     *store.Store
	ipt       *iptables.Executor
	auth      *auth.Auth
	version   string
	upgradeMu sync.Mutex
	upgrade   UpgradeState
}

// UpgradeState 自升级任务状态（供前端轮询展示进度）。
type UpgradeState struct {
	Running  bool   `json:"running"`  // 是否有任务进行中
	Phase    string `json:"phase"`    // downloading/verifying/replacing/restarting/done/error
	Progress int    `json:"progress"` // 0-100
	Msg      string `json:"msg"`      // 进度文案
	Error    string `json:"error"`    // 失败原因（phase=error 时）
}

// New 创建 HTTP 服务并注册路由。webContent 为内嵌的 web/ 目录（单二进制自带前端）。
func New(st *store.Store, ipt *iptables.Executor, a *auth.Auth, webContent fs.FS, version string) *Server {
	s := &Server{store: st, ipt: ipt, auth: a, version: version}

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// 静态页面（web 内嵌进二进制，部署不再依赖外部 web/ 目录）
	// 注意: 不走 http.FileServer/FileFromFS——它对 embed 子目录返回 301 重定向，根路径会重定向循环；
	// 直接 fs.ReadFile 输出内容，与原先 c.File 行为一致。
	webSub, err := fs.Sub(webContent, "web")
	if err != nil {
		panic("web 资源内嵌失败: " + err.Error())
	}
	r.StaticFS("/static", http.FS(webSub))
	r.GET("/", func(c *gin.Context) {
		content, err := fs.ReadFile(webSub, "index.html")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", content)
	})

	// 登录
	r.POST("/api/login", s.handleLogin)

	// 需登录的接口
	authGroup := r.Group("/api", s.authMiddleware())
	{
		authGroup.GET("/rules", s.handleListRules)
		authGroup.POST("/rule", s.handleUpsertRule)
		authGroup.DELETE("/rule/:port", s.handleDeleteRule)
		authGroup.POST("/rule/:port/ip", s.handleAddIP)
		authGroup.DELETE("/rule/:port/ip/:ip", s.handleDelIP)
		authGroup.POST("/rule/:port/strict", s.handleSetStrict)
		authGroup.GET("/me", s.handleMe)
		authGroup.POST("/logout", s.handleLogout)
		authGroup.GET("/sync", s.handleSync)
		authGroup.POST("/change-password", s.handleChangePassword)
		authGroup.GET("/server-info", s.handleServerInfo)
		authGroup.GET("/login-logs", s.handleLoginLogs)
		authGroup.GET("/upgrade/check", s.handleUpgradeCheck)
		authGroup.POST("/upgrade", s.handleUpgrade)
		authGroup.GET("/upgrade/status", s.handleUpgradeStatus)
	}

	s.router = r
	return s
}

// Handler 返回 gin 引擎（供 main 启动）。
func (s *Server) Handler() http.Handler { return s.router }

// Run 启动 HTTP 服务，addr 形如 "0.0.0.0:10443"。
func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}

// authMiddleware 校验 JWT（从 HttpOnly Cookie 或 header 读取）。
func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := s.tokenFromRequest(c)
		username, err := s.auth.Verify(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "msg": "未登录或会话过期"})
			c.Abort()
			return
		}
		c.Set("token", token)
		c.Set("username", username)
		c.Next()
	}
}

// tokenFromRequest 从 Cookie 或 header 提取 JWT。
func (s *Server) tokenFromRequest(c *gin.Context) string {
	if token, err := c.Cookie("ipaw_token"); err == nil && token != "" {
		return token
	}
	if token := c.GetHeader("X-Auth-Token"); token != "" {
		return token
	}
	return c.Query("token")
}

// secureCookie 判断是否应设 Secure：仅 HTTPS（TLS 或 nginx 反代 X-Forwarded-Proto: https）时设。
// 否则裸 HTTP 部署下浏览器不发送 Secure cookie，登录后所有请求 401、前端跳回登录页。
func secureCookie(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	return strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
}

// setTokenCookie 写入 HttpOnly Cookie。
func (s *Server) setTokenCookie(c *gin.Context, token string, remember bool) {
	// 短会话 24h，长会话 30 天
	maxAge := 24 * 3600
	if remember {
		maxAge = 30 * 24 * 3600
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("ipaw_token", token, maxAge, "/", "", secureCookie(c), true)
}

// clearTokenCookie 清除 token Cookie。
func (s *Server) clearTokenCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("ipaw_token", "", -1, "/", "", secureCookie(c), true)
}

// clientIP 获取客户端真实 IP（含 X-Forwarded-For）。
func clientIP(c *gin.Context) string {
	ip := c.ClientIP()
	// 从 X-Real-IP / X-Forwarded-For 取真实来源
	if rip := c.GetHeader("X-Real-IP"); rip != "" {
		ip = rip
	} else if fwd := c.GetHeader("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		ip = strings.TrimSpace(parts[0])
	}
	if idx := strings.Index(ip, ":"); idx >= 0 {
		ip = ip[:idx]
	}
	return ip
}

// ===== Handler =====

type loginReq struct {
	Username string `json:"username" example:"admin"` // 用户名
	Password string `json:"password" example:"***"`   // 密码
	Remember bool   `json:"remember" example:"true"`  // 记住我（长会话）
}

func (s *Server) handleLogin(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "参数错误"})
		return
	}
	// 记录登录日志（成功与失败都记，失败含暴力破解探测）
	recordLogin := func(success bool) {
		if err := s.store.AddLoginLog(store.LoginLog{
			Time:     time.Now().Format(time.RFC3339),
			IP:       clientIP(c),
			Success:  success,
			Username: req.Username,
		}); err != nil {
			log.Printf("记录登录日志失败: %v", err)
		}
	}
	token, err := s.auth.Login(req.Username, req.Password, req.Remember)
	if err != nil {
		recordLogin(false)
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "msg": err.Error()})
		return
	}
	recordLogin(true)
	// 签发 JWT 写入 HttpOnly Cookie（防 XSS 偷 token）
	s.setTokenCookie(c, token, req.Remember)
	c.JSON(http.StatusOK, gin.H{"code": 1, "data": gin.H{"token": token}})
}

// handleLoginLogs 返回最近登录记录（最早的在前）。
func (s *Server) handleLoginLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 1, "data": s.store.GetLoginLogs()})
}

// ===== 自升级 =====

// handleUpgradeCheck 检查是否有新版本（对比 GitHub 最新 release）。
// 本地版本始终返回（不依赖网络），GitHub 查询失败时仅带 check_error，不影响版本号展示。
func (s *Server) handleUpgradeCheck(c *gin.Context) {
	latest, err := fetchLatestVersion()
	checkErr := ""
	if err != nil {
		checkErr = err.Error()
	}
	c.JSON(http.StatusOK, gin.H{"code": 1, "data": gin.H{
		"current":     s.version,
		"latest":      latest,
		"has_update":  latest != "" && versionNewer(latest, s.version),
		"check_error": checkErr,
	}})
}

// handleUpgrade 启动异步自升级（下载→校验→备份→原子替换→重启），立即返回，进度经 /upgrade/status 轮询。
func (s *Server) handleUpgrade(c *gin.Context) {
	s.upgradeMu.Lock()
	if s.upgrade.Running {
		s.upgradeMu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"code": 0, "msg": "升级已在进行中"})
		return
	}
	s.upgrade = UpgradeState{Running: true, Phase: "downloading", Progress: 0, Msg: "开始下载新版本"}
	s.upgradeMu.Unlock()

	go func() {
		err := s.runUpgrade()
		s.upgradeMu.Lock()
		s.upgrade.Running = false
		if err != nil {
			s.upgrade.Phase = "error"
			s.upgrade.Error = err.Error()
			s.upgrade.Msg = "升级失败: " + err.Error()
		} else {
			s.upgrade.Phase = "done"
			s.upgrade.Progress = 100
			s.upgrade.Msg = "升级成功，服务重启中，请稍后刷新"
		}
		s.upgradeMu.Unlock()
	}()
	c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "升级已开始"})
}

// handleUpgradeStatus 返回当前升级状态（前端轮询）。
func (s *Server) handleUpgradeStatus(c *gin.Context) {
	s.upgradeMu.Lock()
	st := s.upgrade
	s.upgradeMu.Unlock()
	c.JSON(http.StatusOK, gin.H{"code": 1, "data": st})
}

// setUpgradeProgress 更新升级进度状态。
func (s *Server) setUpgradeProgress(phase string, progress int, msg string) {
	s.upgradeMu.Lock()
	s.upgrade.Phase = phase
	s.upgrade.Progress = progress
	s.upgrade.Msg = msg
	s.upgradeMu.Unlock()
}

// runUpgrade 执行自升级全流程（goroutine 中运行）。
func (s *Server) runUpgrade() error {
	arch := mapArch(runtime.GOARCH)
	if arch == "" {
		return fmt.Errorf("不支持的架构: %s", runtime.GOARCH)
	}
	asset := fmt.Sprintf("ip-allowlist-linux-%s", arch)
	// 默认走 GitHub 官方源，下载失败时自动 fallback 到 IPAW_MIRROR 镜像（国内 GitHub 受限场景）
	urls := []string{fmt.Sprintf("https://github.com/ins199/ip-allowlist/releases/latest/download/%s", asset)}
	if mirror := os.Getenv("IPAW_MIRROR"); mirror != "" {
		urls = append(urls, strings.TrimRight(mirror, "/")+"/"+asset)
	}

	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位当前二进制失败: %w", err)
	}

	s.setUpgradeProgress("downloading", 0, "正在下载新版本...")
	// 临时文件与目标二进制放同一目录（同一文件系统），保证 rename 原子替换不跨设备（/tmp 常为独立挂载点）
	tmp, srcURL, err := s.downloadToTemp(urls, filepath.Dir(binPath))
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer os.Remove(tmp)

	// SHA256 校验（与下载源同源拉 SHA256SUMS，防镜像投毒；不执行不可信代码）
	s.setUpgradeProgress("verifying", 85, "校验二进制（SHA256）")
	if err := s.verifyChecksum(srcURL, asset, tmp); err != nil {
		return fmt.Errorf("校验失败: %w", err)
	}
	s.setUpgradeProgress("verifying", 90, "校验二进制")
	out, err := exec.Command(tmp, "-version").Output()
	if err != nil || !strings.Contains(string(out), "ip-allowlist") {
		return fmt.Errorf("下载的二进制无效")
	}

	s.setUpgradeProgress("replacing", 95, "备份并替换二进制")
	bak := binPath + ".bak"
	_ = os.Remove(bak)
	if err := os.Rename(binPath, bak); err != nil {
		return fmt.Errorf("备份当前二进制失败: %w", err)
	}
	if err := os.Rename(tmp, binPath); err != nil {
		_ = os.Rename(bak, binPath) // 替换失败回滚备份
		return fmt.Errorf("替换二进制失败: %w", err)
	}
	_ = os.Chmod(binPath, 0755)

	s.setUpgradeProgress("restarting", 98, "重启服务")
	go func() {
		time.Sleep(800 * time.Millisecond)
		if err := exec.Command("systemctl", "restart", "ip-allowlist").Run(); err != nil {
			log.Printf("自升级重启失败: %v", err)
		}
	}()
	return nil
}

// versionNewer 判断版本 a 是否比 b 新（语义化版本，支持 v1.2.3 形式）。
func versionNewer(a, b string) bool {
	if a == "" {
		return false
	}
	if b == "" {
		return true
	}
	an, bn := versionNums(a), versionNums(b)
	for i := 0; i < len(an) && i < len(bn); i++ {
		if an[i] != bn[i] {
			return an[i] > bn[i]
		}
	}
	return len(an) > len(bn)
}

// versionNums 将版本字符串解析为数字切片（忽略非数字段）。
func versionNums(v string) []int {
	var out []int
	for _, p := range strings.Split(strings.TrimPrefix(v, "v"), ".") {
		var seg string
		for _, r := range p {
			if r >= '0' && r <= '9' {
				seg += string(r)
			} else {
				break
			}
		}
		if seg == "" {
			out = append(out, 0)
			continue
		}
		n, _ := strconv.Atoi(seg)
		out = append(out, n)
	}
	return out
}

// fetchLatestVersion 从 GitHub API 查询最新 release tag。
func fetchLatestVersion() (string, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/repos/ins199/ip-allowlist/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ip-allowlist")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API HTTP %d", resp.StatusCode)
	}
	var r struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	return r.TagName, nil
}

// progressReader 包装响应体，统计已读字节并回调下载进度百分比。
type progressReader struct {
	r          io.Reader
	total      int64
	read       int64
	onProgress func(pct int)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	pr.read += int64(n)
	if pr.total > 0 {
		pr.onProgress(int(pr.read * 100 / pr.total))
	}
	return n, err
}

// downloadToTemp 依次尝试多个下载源（默认 GitHub 官方，fallback 镜像），成功即返回（含成功源 URL）；全部失败返回最后错误。
// 每个源短超时（10s）：正常网络下 10s 足够下载 10MB，国内 GitHub 被阻断时快速 fallback 镜像。
func (s *Server) downloadToTemp(urls []string, dir string) (string, string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	var lastErr error
	for _, url := range urls {
		tmp, err := s.downloadOne(client, url, dir)
		if err == nil {
			return tmp, url, nil
		}
		lastErr = err
		log.Printf("自升级从 %s 下载失败，尝试下一个源: %v", url, err)
	}
	return "", "", lastErr
}

// verifyChecksum 从与下载源同目录拉取 SHA256SUMS，校验下载文件的完整性（防镜像投毒）。
func (s *Server) verifyChecksum(srcURL, asset, file string) error {
	base := strings.TrimSuffix(srcURL, path.Base(srcURL))
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(base + "SHA256SUMS")
	if err != nil {
		return fmt.Errorf("获取校验和失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("校验和文件 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	expected := ""
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == asset {
			expected = fields[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("校验和文件缺少 %s", asset)
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		f.Close()
		return err
	}
	f.Close()
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("SHA256 校验失败（期望 %s，实际 %s）", expected, actual)
	}
	log.Printf("自升级 SHA256 校验通过: %s", asset)
	return nil
}

// downloadOne 从单个 URL 下载到 dir 目录下的临时路径，上报进度，校验非空且大于 1MB。
func (s *Server) downloadOne(client *http.Client, url, dir string) (string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	tmp := filepath.Join(dir, fmt.Sprintf(".ipaw-upgrade-%d", os.Getpid()))
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	pr := &progressReader{r: resp.Body, total: resp.ContentLength, onProgress: func(pct int) {
		s.setUpgradeProgress("downloading", pct, fmt.Sprintf("正在下载新版本... %d%%", pct))
	}}
	_, err = io.Copy(f, pr)
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	fi, err := os.Stat(tmp)
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	if fi.Size() < 1024*1024 {
		os.Remove(tmp)
		return "", fmt.Errorf("文件过小(%d 字节)，疑似错误响应", fi.Size())
	}
	_ = os.Chmod(tmp, 0755)
	return tmp, nil
}

// mapArch 将 GOARCH 映射为 Release 资产名中的架构标识。
func mapArch(goarch string) string {
	switch goarch {
	case "amd64", "arm64":
		return goarch
	}
	return ""
}

type changePassReq struct {
	OldPassword string `json:"old_password" example:"***"` // 旧密码
	NewPassword string `json:"new_password" example:"***"` // 新密码
}

func (s *Server) handleChangePassword(c *gin.Context) {
	var req changePassReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "参数错误"})
		return
	}
	if err := s.auth.ChangePassword(req.OldPassword, req.NewPassword); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "密码已修改，请重新登录"})
}

func (s *Server) handleLogout(c *gin.Context) {
	// JWT 无状态，注销即清除 Cookie
	s.clearTokenCookie(c)
	c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "已退出"})
}

// ResForRule 端口规则响应。
type ResForRule struct {
	Port      int               `json:"port"`       // 端口号
	Comment   string            `json:"comment"`    // 用途说明
	Strict    bool              `json:"strict"`     // 严格模式
	AllowList []store.AllowItem `json:"allow_list"` // 白名单
	CurrentIP string            `json:"current_ip"` // 当前请求来源 IP
	DropOn    bool              `json:"drop_on"`    // INPUT 是否已启用 DROP
}

func (s *Server) handleListRules(c *gin.Context) {
	rules := s.store.GetRules()
	out := make([]ResForRule, 0, len(rules))
	cur := clientIP(c)
	for _, r := range rules {
		out = append(out, ResForRule{
			Port:      r.Port,
			Comment:   r.Comment,
			Strict:    r.Strict,
			AllowList: r.AllowList,
			CurrentIP: cur,
			DropOn:    s.ipt.PortDropActive(r.Port),
		})
	}
	c.JSON(http.StatusOK, gin.H{"code": 1, "data": out})
}

// ResForMe 当前登录信息。
type ResForMe struct {
	Username  string `json:"username"`   // 用户名
	CurrentIP string `json:"current_ip"` // 当前来源 IP
}

func (s *Server) handleMe(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 1, "data": ResForMe{
		Username:  c.GetString("username"),
		CurrentIP: clientIP(c),
	}})
}

// persistCurrentIP 严格模式下将当前来源 IP 持久化加入白名单，返回最新规则。
// 让「编辑/创建规则」与「切换严格模式」两条路径的防锁死行为保持一致（重启不丢当前 IP）。
func (s *Server) persistCurrentIP(c *gin.Context, port int, rule store.PortRule) store.PortRule {
	if !rule.Strict {
		return rule
	}
	cur := clientIP(c)
	if cur == "" || !iptables.ValidIPorCIDR(cur) {
		return rule
	}
	for _, item := range rule.AllowList {
		if item.IP == cur {
			return rule
		}
	}
	if _, err := s.store.AddIP(port, cur, "auto"); err != nil {
		return rule
	}
	if updated := s.store.GetRule(port); updated != nil {
		return *updated
	}
	return rule
}

type ruleReq struct {
	Port    int    `json:"port" example:"22"`     // 端口号
	Comment string `json:"comment" example:"SSH"` // 用途说明
	Strict  bool   `json:"strict" example:"true"` // 严格模式
}

func (s *Server) handleUpsertRule(c *gin.Context) {
	var req ruleReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Port <= 0 || req.Port > 65535 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "端口非法"})
		return
	}
	rule := store.PortRule{Port: req.Port, Comment: req.Comment, Strict: req.Strict}
	// 保留已有白名单
	if old := s.store.GetRule(req.Port); old != nil {
		rule.AllowList = old.AllowList
	}
	if err := s.store.UpsertRule(rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error()})
		return
	}
	// 严格模式下补当前来源 IP 持久化（防锁死，与切换严格模式路径一致）
	rule = s.persistCurrentIP(c, req.Port, rule)
	if err := s.ipt.ApplyPortRule(rule, clientIP(c)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": "保存成功但应用iptables失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "规则已保存并应用"})
}

func (s *Server) handleDeleteRule(c *gin.Context) {
	port, err := strconv.Atoi(c.Param("port"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "端口非法"})
		return
	}
	if err := s.store.DeleteRule(port); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error()})
		return
	}
	// 清理 iptables（重建空链 + 移除引用），失败需告知避免配置与规则不一致
	if err := s.ipt.ApplyPortRule(store.PortRule{Port: port, Strict: false}, clientIP(c)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": "规则已删除但清理iptables失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "规则已删除"})
}

type addIPReq struct {
	IP     string `json:"ip" example:"1.2.3.4"` // IP 或 CIDR
	Remark string `json:"remark" example:"公司"`  // 备注
}

func (s *Server) handleAddIP(c *gin.Context) {
	port, err := strconv.Atoi(c.Param("port"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "端口非法"})
		return
	}
	var req addIPReq
	if err := c.ShouldBindJSON(&req); err != nil || !iptables.ValidIPorCIDR(strings.TrimSpace(req.IP)) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "IP或CIDR非法"})
		return
	}
	req.IP = strings.TrimSpace(req.IP)
	added, err := s.store.AddIP(port, req.IP, req.Remark)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error()})
		return
	}
	if !added {
		c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "IP已在白名单中"})
		return
	}
	// 应用后重读最新规则
	rule := s.store.GetRule(port)
	if rule == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": "规则不存在"})
		return
	}
	if err := s.ipt.ApplyPortRule(*rule, clientIP(c)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": "保存成功但应用iptables失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "已添加并应用"})
}

func (s *Server) handleDelIP(c *gin.Context) {
	port, err := strconv.Atoi(c.Param("port"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "端口非法"})
		return
	}
	ip := c.Param("ip")
	cur := clientIP(c)

	// 防锁死：严格模式下禁止删除当前来源 IP
	rule := s.store.GetRule(port)
	if rule != nil && rule.Strict && ip == cur {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "严格模式下禁止删除当前来源 IP（会锁死）"})
		return
	}

	deleted, err := s.store.DelIP(port, ip)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error()})
		return
	}
	if !deleted {
		c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "IP 不在白名单中"})
		return
	}
	// 防锁死：严格模式且删后白名单为空 → 拒绝（回滚）
	rule = s.store.GetRule(port)
	if rule != nil && rule.Strict && len(rule.AllowList) == 0 {
		// 回滚：重新加回
		_, _ = s.store.AddIP(port, ip, "")
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "严格模式至少保留一条白名单 IP（已回滚）"})
		return
	}
	if rule == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": "规则不存在"})
		return
	}
	if err := s.ipt.ApplyPortRule(*rule, cur); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": "保存成功但应用iptables失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "已删除并应用"})
}

type strictReq struct {
	Strict bool `json:"strict" example:"true"` // 是否严格模式
}

func (s *Server) handleSetStrict(c *gin.Context) {
	port, err := strconv.Atoi(c.Param("port"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "端口非法"})
		return
	}
	var req strictReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "参数错误"})
		return
	}
	if err := s.store.SetStrict(port, req.Strict); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error()})
		return
	}
	rule := s.store.GetRule(port)
	if rule == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": "规则不存在"})
		return
	}
	// 开启严格模式时：将当前来源 IP 持久化加入白名单（防锁死，且重启不丢）
	ruleVal := s.persistCurrentIP(c, port, *rule)
	if err := s.ipt.ApplyPortRule(ruleVal, clientIP(c)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": "保存成功但应用iptables失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "严格模式已更新"})
}

// handleSync 将配置与 iptables 现状对齐（供页面手动触发/定时调用）。
func (s *Server) handleSync(c *gin.Context) {
	rules := s.store.GetRules()
	errs := s.ipt.Reconcile(rules, clientIP(c))
	if len(errs) > 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": fmt.Sprintf("同步失败 %d 个规则", len(errs)), "errors": errs})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "同步完成"})
}

// handleServerInfo 返回服务器基础运维信息。
func (s *Server) handleServerInfo(c *gin.Context) {
	info := sysinfo.Collect()
	c.JSON(http.StatusOK, gin.H{"code": 1, "data": info})
}
