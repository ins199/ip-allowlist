package iptables

import (
	"os"
	"testing"

	"ip-allowlist/internal/store"
)

// 所有测试在 dry-run 模式运行，不真正改动防火墙
func TestMain(m *testing.M) {
	os.Setenv("IPAW_DRY_RUN", "1")
	code := m.Run()
	os.Unsetenv("IPAW_DRY_RUN")
	os.Exit(code)
}

func TestValidIPorCIDR(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"1.2.3.4", true},
		{"1.2.3.0/24", true},
		{"203.0.113.10/32", true},
		{"not-an-ip", false},
		{"", false},
		{"1.2.3.999", false},
	}
	for _, c := range cases {
		if got := ValidIPorCIDR(c.in); got != c.want {
			t.Errorf("ValidIPorCIDR(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestChainName(t *testing.T) {
	if got := chainName(22); got != "IPAW-22" {
		t.Errorf("chainName(22) = %q, want IPAW-22", got)
	}
	if got := chainName(10443); got != "IPAW-10443" {
		t.Errorf("chainName(10443) = %q, want IPAW-10443", got)
	}
}

func TestApplyPortRuleStrictEmptyReject(t *testing.T) {
	e := New()
	err := e.ApplyPortRule(store.PortRule{Port: 22, Strict: true}, "")
	if err == nil {
		t.Fatal("严格模式空白名单应被拒绝（防锁死）")
	}
}

func TestApplyPortRuleLooseEmptyOK(t *testing.T) {
	e := New()
	err := e.ApplyPortRule(store.PortRule{Port: 22, Strict: false}, "")
	if err != nil {
		t.Fatalf("宽松模式空白名单应允许: %v", err)
	}
}

func TestApplyPortRuleAutoAddCurrentIP(t *testing.T) {
	e := New()
	rule := store.PortRule{
		Port:   22,
		Strict: true,
		AllowList: []store.AllowItem{
			{IP: "1.2.3.4", Remark: "已有"},
		},
	}
	err := e.ApplyPortRule(rule, "5.6.7.8")
	if err != nil {
		t.Fatalf("严格模式非空白名单应可应用: %v", err)
	}
}

func TestApplyPortRuleCurrentIPAlreadyListed(t *testing.T) {
	e := New()
	rule := store.PortRule{
		Port:   22,
		Strict: true,
		AllowList: []store.AllowItem{
			{IP: "1.2.3.4", Remark: "已有"},
		},
	}
	// 当前 IP 已在白名单，不应重复
	err := e.ApplyPortRule(rule, "1.2.3.4")
	if err != nil {
		t.Fatalf("应用失败: %v", err)
	}
}
