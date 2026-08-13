// Package auth 提供管理后台的账号密码鉴权与会话管理。
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Auth 鉴权管理器，持有用户凭据和会话。
type Auth struct {
	mu          sync.RWMutex
	username    string
	passHash    []byte
	sessions    map[string]time.Time // token -> 过期时间
	sessionTTL  time.Duration
	longSessionTTL time.Duration
}

// New 创建鉴权管理器。password 为明文，内部 bcrypt 哈希存储。
// longTTL 为"记住我"会话时长。
func New(username, password string, sessionTTL, longTTL time.Duration) (*Auth, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return &Auth{
		username:       username,
		passHash:       hash,
		sessions:       make(map[string]time.Time),
		sessionTTL:     sessionTTL,
		longSessionTTL: longTTL,
	}, nil
}

// Login 校验用户名密码，成功返回 session token。remember=true 使用长会话。
func (a *Auth) Login(username, password string, remember bool) (string, error) {
	if username != a.username {
		return "", ErrUnauthorized
	}
	if bcrypt.CompareHashAndPassword(a.passHash, []byte(password)) != nil {
		return "", ErrUnauthorized
	}
	token := randomToken()
	ttl := a.sessionTTL
	if remember {
		ttl = a.longSessionTTL
	}
	a.mu.Lock()
	a.sessions[token] = time.Now().Add(ttl)
	a.mu.Unlock()
	return token, nil
}

// ChangePassword 修改密码（旧密码校验）。
func (a *Auth) ChangePassword(oldPass, newPass string) error {
	if bcrypt.CompareHashAndPassword(a.passHash, []byte(oldPass)) != nil {
		return ErrUnauthorized
	}
	if len(newPass) < 6 {
		return &AuthError{"新密码至少 6 位"}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPass), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.passHash = hash
	// 修改密码后清除所有会话，强制重新登录
	a.sessions = make(map[string]time.Time)
	a.mu.Unlock()
	return nil
}

// Verify 校验 session token 是否有效。
func (a *Auth) Verify(token string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	exp, ok := a.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(a.sessions, token)
		return false
	}
	return true
}

// Logout 注销 session。
func (a *Auth) Logout(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.sessions, token)
}

// randomToken 生成随机 session token。
func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// hashString 辅助：对任意字符串做 SHA256（用于配置值混淆，非登录凭据）。
func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ErrUnauthorized 认证失败错误。
var ErrUnauthorized = &AuthError{"用户名或密码错误"}

// AuthError 鉴权错误。
type AuthError struct{ msg string }

func (e *AuthError) Error() string { return e.msg }
