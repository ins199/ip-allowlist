// Package iptables 负责生成并应用本机 iptables 白名单规则。
//
// 设计目标：同一台宿主机上对多个端口做 IP 白名单管理。
// 规则结构：每个端口一条独立链 IPAW-<port>，INPUT 链按端口引用，严格模式追加 DROP 兜底。
// 安全铁律：
//  1. 先 ACCEPT 后 DROP，顺序不能反。
//  2. 严格模式应用前必须确保白名单非空，否则拒绝应用（防锁死）。
//  3. 应用走"重建"而非逐条增删，保证规则顺序正确、可幂等重放。
//  4. 支持 dry-run 模式：只打印将执行的命令，不真正改动防火墙（本地测试用）。
package iptables

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"ip-allowlist/internal/store"
)

// DryRun 是否只打印不执行。通过环境变量 IPAW_DRY_RUN=1 开启，生产不设置。
func DryRun() bool {
	return os.Getenv("IPAW_DRY_RUN") == "1"
}

// Executor 执行 iptables 命令；dry-run 下仅打印。
type Executor struct {
	mu sync.Mutex
}

// New 创建 iptables 执行器。
func New() *Executor { return &Executor{} }

// run 执行单条 iptables 命令。
func (e *Executor) run(args ...string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if DryRun() {
		fmt.Printf("[dry-run] iptables %s\n", strings.Join(args, " "))
		return nil
	}
	cmd := exec.Command("iptables", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("iptables %s: %v: %s", strings.Join(args, " "), err, string(out))
	}
	return nil
}

// chainName 端口对应的链名。
func chainName(port int) string { return fmt.Sprintf("IPAW-%d", port) }

// ValidIPorCIDR 校验 IP 或 CIDR 是否合法 IPv4。
func ValidIPorCIDR(ip string) bool {
	if strings.Contains(ip, "/") {
		_, _, err := net.ParseCIDR(ip)
		return err == nil
	}
	return net.ParseIP(ip) != nil
}

// NormalizeIP 纯 IP 转 /32，CIDR 原样。
func NormalizeIP(ip string) string {
	if strings.Contains(ip, "/") {
		return ip
	}
	return ip + "/32"
}

// ruleArgs 生成一条链内的 ACCEPT 规则参数。
func ruleArgs(chain string, ip string) []string {
	return []string{"-A", chain, "-s", NormalizeIP(ip), "-j", "ACCEPT"}
}

// ApplyPortRule 应用单条端口规则：
//   - 重建该端口的 IPAW 链（白名单 ACCEPT + RETURN）
//   - 更新 INPUT 链对该端口的引用和严格模式 DROP 兜底
//
// strict 模式下若白名单为空则拒绝应用（防锁死）。
func (e *Executor) ApplyPortRule(rule store.PortRule, currentIP string) error {
	chain := chainName(rule.Port)

	// 严格模式但白名单为空：拒绝（防锁死）
	if rule.Strict && len(rule.AllowList) == 0 {
		return fmt.Errorf("端口 %d 严格模式但白名单为空，拒绝应用（防锁死）", rule.Port)
	}

	// 严格模式下：当前来源 IP 若不在白名单，自动补入（防锁死）
	if rule.Strict && currentIP != "" && ValidIPorCIDR(currentIP) {
		has := false
		for _, item := range rule.AllowList {
			if item.IP == currentIP {
				has = true
				break
			}
		}
		if !has {
			rule.AllowList = append(rule.AllowList, store.AllowItem{IP: currentIP, Remark: "auto(当前来源)"})
		}
	}

	// 1. 删除旧链（先删 INPUT 引用再删链）
	_ = e.run("-D", "INPUT", "-p", "tcp", "--dport", fmt.Sprint(rule.Port), "-j", chain)
	_ = e.run("-D", "INPUT", "-p", "tcp", "--dport", fmt.Sprint(rule.Port), "-j", "DROP")
	_ = e.run("-F", chain)
	_ = e.run("-X", chain)

	// 2. 重建链
	if err := e.run("-N", chain); err != nil {
		return err
	}

	// 3. 白名单 ACCEPT（先放行）
	for _, item := range rule.AllowList {
		if err := e.run(ruleArgs(chain, item.IP)...); err != nil {
			return err
		}
	}

	// 4. RETURN 回 INPUT
	if err := e.run("-A", chain, "-j", "RETURN"); err != nil {
		return err
	}

	// 5. INPUT 引用该链（插到最前，优先于 fail2ban 等）
	if err := e.run("-I", "INPUT", "-p", "tcp", "--dport", fmt.Sprint(rule.Port), "-j", chain); err != nil {
		return err
	}

	// 6. 严格模式追加 DROP 兜底
	if rule.Strict {
		if err := e.run("-A", "INPUT", "-p", "tcp", "--dport", fmt.Sprint(rule.Port), "-j", "DROP"); err != nil {
			return err
		}
	}

	return nil
}

// ApplyAll 应用全部规则。返回每个端口的应用结果。
func (e *Executor) ApplyAll(rules []store.PortRule, currentIP string) []error {
	errs := make([]error, 0, len(rules))
	for _, rule := range rules {
		if err := e.ApplyPortRule(rule, currentIP); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// CurrentChainRules 读取指定端口链当前的 IP 列表（dry-run 下返回 nil）。
func (e *Executor) CurrentChainRules(port int) []string {
	if DryRun() {
		return nil
	}
	out, err := exec.Command("iptables", "-S", chainName(port)).CombinedOutput()
	if err != nil {
		return nil
	}
	re := regexp.MustCompile(`-s ([0-9./]+)`)
	var ips []string
	for _, line := range strings.Split(string(out), "\n") {
		if m := re.FindStringSubmatch(line); m != nil {
			ips = append(ips, m[1])
		}
	}
	return ips
}

// PortDropActive 判断指定端口 INPUT 是否启用了严格模式 DROP 兜底。
func (e *Executor) PortDropActive(port int) bool {
	if DryRun() {
		return false
	}
	out, err := exec.Command("iptables", "-S", "INPUT").CombinedOutput()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "--dport "+fmt.Sprint(port)) && strings.Contains(line, "-j DROP") {
			return true
		}
	}
	return false
}

// Reconcile 将 iptables 现状与配置对齐（应用缺失的、移除多余的）。
// 主要供开机恢复/定时同步调用。
func (e *Executor) Reconcile(rules []store.PortRule, currentIP string) []error {
	return e.ApplyAll(rules, currentIP)
}
