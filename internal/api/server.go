// Package api 提供 HTTP API 和 Web 页面服务。
package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"ip-allowlist/internal/auth"
	"ip-allowlist/internal/iptables"
	"ip-allowlist/internal/store"
	"ip-allowlist/internal/sysinfo"
)

// Server HTTP 服务。
type Server struct {
	router *gin.Engine
	store  *store.Store
	ipt    *iptables.Executor
	auth   *auth.Auth
}

// New 创建 HTTP 服务并注册路由。
func New(st *store.Store, ipt *iptables.Executor, a *auth.Auth) *Server {
	s := &Server{store: st, ipt: ipt, auth: a}

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// 静态页面（嵌入 web 目录）
	r.StaticFS("/static", http.Dir("./web"))
	r.GET("/", func(c *gin.Context) {
		c.File("./web/index.html")
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

// setTokenCookie 写入 HttpOnly Cookie。
func (s *Server) setTokenCookie(c *gin.Context, token string, remember bool) {
	// 短会话 24h，长会话 30 天
	maxAge := 24 * 3600
	if remember {
		maxAge = 30 * 24 * 3600
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("ipaw_token", token, maxAge, "/", "", true, true) // Secure + HttpOnly
}

// clearTokenCookie 清除 token Cookie。
func (s *Server) clearTokenCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("ipaw_token", "", -1, "/", "", true, true)
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
	token, err := s.auth.Login(req.Username, req.Password, req.Remember)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "msg": err.Error()})
		return
	}
	// 签发 JWT 写入 HttpOnly Cookie（防 XSS 偷 token）
	s.setTokenCookie(c, token, req.Remember)
	c.JSON(http.StatusOK, gin.H{"code": 1, "data": gin.H{"token": token}})
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
	if _, err := s.store.AddIP(port, cur, "auto(当前来源)"); err != nil {
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
