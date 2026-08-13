package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 系统配置
type Config struct {
	Server ServerCfg `yaml:"server"` // HTTP 服务配置
	Auth   AuthCfg   `yaml:"auth"`   // 鉴权配置
}

// ServerCfg HTTP 服务配置
type ServerCfg struct {
	Addr     string `yaml:"addr"`      // 监听地址，如 0.0.0.0:8443
	DataFile string `yaml:"data_file"` // 白名单数据文件路径
}

// AuthCfg 鉴权配置
type AuthCfg struct {
	Username     string `yaml:"username"`      // 管理账号
	Password     string `yaml:"password"`      // 管理密码（明文，首次部署后建议修改）
	SessionHours int    `yaml:"session_hours"` // 会话有效期（小时）
	RememberDays int    `yaml:"remember_days"` // "记住我"会话时长（天）
}

// DefaultConfig 返回默认配置。
func DefaultConfig() Config {
	return Config{
		Server: ServerCfg{Addr: "0.0.0.0:10443"},
		Auth:   AuthCfg{Username: "admin", Password: "changeme", SessionHours: 24, RememberDays: 30},
	}
}

// loadConfig 加载 YAML 配置；文件不存在则返回默认值。
func loadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	return cfg, nil
}
