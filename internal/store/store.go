// Package store 负责白名单配置的持久化存储（JSON 文件）。
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// AllowItem 单条白名单规则
type AllowItem struct {
	IP     string `json:"ip"`     // IP 地址或 CIDR，如 1.2.3.4 或 1.2.3.0/24
	Remark string `json:"remark"` // 备注，如"本机宽带"
}

// PortRule 一个端口的白名单规则
type PortRule struct {
	Port      int         `json:"port"`       // 端口号
	Comment   string      `json:"comment"`    // 端口用途说明，如"SSH"
	Strict    bool        `json:"strict"`     // 严格模式：仅白名单 IP 可连；false 为宽松(只展示不阻断)
	AllowList []AllowItem `json:"allow_list"` // 白名单 IP 列表
}

// Config 白名单系统配置
type Config struct {
	Rules      []PortRule `json:"rules"`       // 各端口规则
	LoginLogs  []LoginLog `json:"login_logs"`  // 最近登录记录（保留 MaxLoginLogs 条）
}

// LoginLog 一次登录尝试记录。
type LoginLog struct {
	Time     string `json:"time"`     // 登录时间（RFC3339）
	IP       string `json:"ip"`       // 来源 IP
	Success  bool   `json:"success"`  // 是否成功
	Username string `json:"username"` // 尝试登录的用户名
}

// MaxLoginLogs 登录记录保留条数。
const MaxLoginLogs = 50

// Store 白名单存储，带锁保护并发读写
type Store struct {
	mu    sync.RWMutex
	path  string
	cfg   Config
	dirty bool
}

// New 创建 Store 并加载配置文件（不存在则创建空配置）。
func New(path string) (*Store, error) {
	s := &Store{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.cfg = Config{}
			return s.Save()
		}
		return fmt.Errorf("读取配置失败: %w", err)
	}
	if err := json.Unmarshal(data, &s.cfg); err != nil {
		return fmt.Errorf("解析配置失败: %w", err)
	}
	return nil
}

// Save 保存配置到磁盘，目录不存在时自动创建。
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

// GetRules 返回当前所有规则（副本）。
func (s *Store) GetRules() []PortRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PortRule, len(s.cfg.Rules))
	copy(out, s.cfg.Rules)
	return out
}

// GetRule 返回指定端口的规则，不存在返回 nil。
func (s *Store) GetRule(port int) *PortRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.cfg.Rules {
		if s.cfg.Rules[i].Port == port {
			// 返回副本
			r := s.cfg.Rules[i]
			return &r
		}
	}
	return nil
}

// UpsertRule 新增或更新端口规则并保存。
func (s *Store) UpsertRule(rule PortRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.Rules {
		if s.cfg.Rules[i].Port == rule.Port {
			s.cfg.Rules[i] = rule
			return s.saveLocked()
		}
	}
	s.cfg.Rules = append(s.cfg.Rules, rule)
	return s.saveLocked()
}

// DeleteRule 删除端口规则并保存。
func (s *Store) DeleteRule(port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.Rules {
		if s.cfg.Rules[i].Port == port {
			s.cfg.Rules = append(s.cfg.Rules[:i], s.cfg.Rules[i+1:]...)
			return s.saveLocked()
		}
	}
	return nil
}

// AddIP 向指定端口白名单添加 IP（去重），并保存。返回是否新增了。
func (s *Store) AddIP(port int, ip, remark string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i := range s.cfg.Rules {
		if s.cfg.Rules[i].Port == port {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.cfg.Rules = append(s.cfg.Rules, PortRule{
			Port:      port,
			AllowList: []AllowItem{{IP: ip, Remark: remark}},
		})
		return true, s.saveLocked()
	}
	for _, item := range s.cfg.Rules[idx].AllowList {
		if item.IP == ip {
			return false, nil // 已存在，去重
		}
	}
	s.cfg.Rules[idx].AllowList = append(s.cfg.Rules[idx].AllowList, AllowItem{IP: ip, Remark: remark})
	return true, s.saveLocked()
}

// DelIP 从指定端口白名单删除 IP，并保存。返回是否删除了。
func (s *Store) DelIP(port int, ip string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.Rules {
		if s.cfg.Rules[i].Port != port {
			continue
		}
		newList := make([]AllowItem, 0, len(s.cfg.Rules[i].AllowList))
		found := false
		for _, item := range s.cfg.Rules[i].AllowList {
			if item.IP == ip {
				found = true
				continue
			}
			newList = append(newList, item)
		}
		if !found {
			return false, nil
		}
		s.cfg.Rules[i].AllowList = newList
		return true, s.saveLocked()
	}
	return false, nil
}

// SetStrict 设置端口严格模式并保存。
func (s *Store) SetStrict(port int, strict bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.Rules {
		if s.cfg.Rules[i].Port == port {
			s.cfg.Rules[i].Strict = strict
			return s.saveLocked()
		}
	}
	return nil
}

// AddLoginLog 追加一条登录记录（保留最近 MaxLoginLogs 条）并保存。
func (s *Store) AddLoginLog(log LoginLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.LoginLogs = append(s.cfg.LoginLogs, log)
	if len(s.cfg.LoginLogs) > MaxLoginLogs {
		s.cfg.LoginLogs = s.cfg.LoginLogs[len(s.cfg.LoginLogs)-MaxLoginLogs:]
	}
	return s.saveLocked()
}

// GetLoginLogs 返回登录记录副本（最早的在前）。
func (s *Store) GetLoginLogs() []LoginLog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]LoginLog, len(s.cfg.LoginLogs))
	copy(out, s.cfg.LoginLogs)
	return out
}
