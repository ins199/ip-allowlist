// ip-allowlist：通用 IP 白名单管理系统。
// 单二进制，部署到任何 Linux 宿主机即用，管理多个端口的 IP 白名单（iptables）。
// 详细说明见 README.md。
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"ip-allowlist/internal/api"
	"ip-allowlist/internal/auth"
	"ip-allowlist/internal/iptables"
	"ip-allowlist/internal/store"
)

// Version 当前版本，发版时打 tag 保持一致。
var Version = "v1.0.8"

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

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	if cfg.Auth.Secret == "change-me-secret" {
		log.Println("警告: JWT secret 仍为默认值，请务必在 config.yaml 中改为随机长字符串")
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
		log.Fatalf("初始化存储失败: %v", err)
	}

	// 鉴权
	a, err := auth.New(cfg.Auth.Username, cfg.Auth.Password, cfg.Auth.Secret,
		time.Duration(cfg.Auth.SessionHours)*time.Hour,
		time.Duration(cfg.Auth.RememberDays)*24*time.Hour)
	if err != nil {
		log.Fatalf("初始化鉴权失败: %v", err)
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

	// HTTP 服务
	srv := api.New(st, ipt, a, webFS, Version)
	log.Printf("ip-allowlist 启动，监听 %s，数据文件 %s", *bindAddr, *dataPath)
	if err := srv.Run(*bindAddr); err != nil {
		log.Fatalf("HTTP 服务启动失败: %v", err)
	}
	os.Exit(0)
}
