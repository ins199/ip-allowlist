// ip-allowlist：通用 IP 白名单管理系统。
// 单二进制，部署到任何 Linux 宿主机即用，管理多个端口的 IP 白名单（iptables）。
// 详细说明见 README.md。
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"ip-allowlist/internal/api"
	"ip-allowlist/internal/auth"
	"ip-allowlist/internal/iptables"
	"ip-allowlist/internal/store"
)

// Version 版本号。发版时由 CI 通过 -ldflags "-X main.Version=<tag>" 注入（如 v1.0.19），
// 本地开发默认 "dev"，无需手动维护。
var Version = "dev"

func main() {
	var (
		configPath  = flag.String("config", "/opt/ip-allowlist/config.yaml", "配置文件路径")
		dataPath    = flag.String("data", "", "白名单数据文件路径（留空则用配置）")
		bindAddr    = flag.String("bind", "", "监听地址（留空则用配置）")
		showVersion = flag.Bool("version", false, "显示版本")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("ip-allowlist %s\n", Version)
		return
	}

	// 自升级回滚标记检测：升级时替换二进制前会写 .ipaw-upgrade-pending，
	// 新版本初始化失败则自动用 .bak 恢复上一版本，避免升级后起不来。
	binPath, _ := os.Executable()
	pendingMark := binPath + ".ipaw-upgrade-pending"
	upgrading := false
	if _, err := os.Stat(pendingMark); err == nil {
		upgrading = true
		log.Println("检测到自升级标记，本次为新版本首次启动")
	}
	fatalRollback := func(format string, args ...interface{}) {
		if upgrading {
			log.Printf("升级后初始化失败，回滚到上一版本: "+format, args...)
			if err := rollbackBinary(binPath); err != nil {
				log.Printf("回滚失败: %v", err)
			}
		}
		log.Fatalf(format, args...)
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fatalRollback("加载配置失败: %v", err)
	}
	if cfg.Auth.Secret == "change-me-secret" {
		fatalRollback("JWT secret 为默认值，拒绝启动：请设置随机 secret（deploy.sh 会自动生成）")
	}
	// flag 显式传入时优先于配置文件
	if *bindAddr == "" && cfg.Server.Addr != "" {
		*bindAddr = cfg.Server.Addr
	}
	if *dataPath == "" && cfg.Server.DataFile != "" {
		*dataPath = cfg.Server.DataFile
	}
	if *bindAddr == "" {
		*bindAddr = "0.0.0.0:10443"
	}
	if *dataPath == "" {
		*dataPath = "/opt/ip-allowlist/allowlist.json"
	}

	// 存储
	st, err := store.New(*dataPath)
	if err != nil {
		fatalRollback("初始化存储失败: %v", err)
	}

	// 鉴权
	a, err := auth.New(cfg.Auth.Username, cfg.Auth.Password, cfg.Auth.Secret,
		time.Duration(cfg.Auth.SessionHours)*time.Hour,
		time.Duration(cfg.Auth.RememberDays)*24*time.Hour)
	if err != nil {
		fatalRollback("初始化鉴权失败: %v", err)
	}
	// 修改密码后写回 config.yaml（重启不丢失）
	a.SetPersistPassword(func(newPassword string) error {
		cfg.Auth.Password = newPassword
		return saveConfig(*configPath, cfg)
	})

	// iptables
	ipt := iptables.New()

	// 首次启动：若无任何规则，自动创建 SSH(22) 默认规则（宽松模式）
	if len(st.GetRules()) == 0 {
		log.Println("首次启动：创建默认 SSH(22) 规则（宽松模式）")
		if err := st.UpsertRule(store.PortRule{
			Port:    22,
			Comment: "SSH",
			Strict:  false,
		}); err != nil {
			log.Printf("创建默认规则失败: %v", err)
		}
	}

	// 启动时同步一次（开机恢复规则）
	if !iptables.DryRun() {
		rules := st.GetRules()
		errs := ipt.Reconcile(rules, "")
		for _, e := range errs {
			log.Printf("启动同步警告: %v", e)
		}
		log.Printf("启动时已同步 %d 条端口规则", len(rules))
	} else {
		log.Println("dry-run 模式：不执行 iptables，仅打印")
	}

	// 初始化全部成功，清除自升级标记（确认升级成功）
	if upgrading {
		if err := os.Remove(pendingMark); err == nil {
			log.Println("自升级成功，已清除升级标记")
		}
	}

	// HTTP 服务
	srv := api.New(st, ipt, a, webFS, Version)
	log.Printf("ip-allowlist 启动，监听 %s，数据文件 %s", *bindAddr, *dataPath)
	if err := srv.Run(*bindAddr); err != nil {
		log.Fatalf("HTTP 服务启动失败: %v", err)
	}
	os.Exit(0)
}

// rollbackBinary 升级后新版本启动失败时，用 .bak 恢复上一版本并重启服务。
func rollbackBinary(binPath string) error {
	bak := binPath + ".bak"
	if _, err := os.Stat(bak); err != nil {
		return fmt.Errorf("备份 .bak 不存在，无法回滚")
	}
	if err := os.Rename(bak, binPath); err != nil {
		return err
	}
	if err := os.Chmod(binPath, 0755); err != nil {
		return err
	}
	log.Printf("已用 .bak 恢复二进制，重启服务加载旧版本")
	return exec.Command("systemctl", "restart", "ip-allowlist").Start()
}
