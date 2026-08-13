// Package auth 提供管理后台的 JWT 鉴权。
// 无状态 JWT：token 自带用户信息 + 过期时间，服务重启不影响已登录用户，多实例共享（同一密钥）。
package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Auth JWT 鉴权管理器。
type Auth struct {
	secret         []byte
	username       string
	passHash       []byte
	shortTTL       time.Duration // 默认会话时长
	longTTL        time.Duration // "记住我"会话时长
	persistPassword func(newPassword string) error
}

// New 创建鉴权管理器。secret 为 JWT 签名密钥。
// password 为明文，内部 bcrypt 哈希存储。
func New(username, password, secret string, sessionTTL, longTTL time.Duration) (*Auth, error) {
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	if secret == "" {
		return nil, fmt.Errorf("JWT secret 不能为空")
	}
	return &Auth{
		secret:   []byte(secret),
		username: username,
		passHash: hash,
		shortTTL: sessionTTL,
		longTTL:  longTTL,
	}, nil
}

// SetPersistPassword 设置密码持久化回调，改密成功后写回配置文件。
func (a *Auth) SetPersistPassword(fn func(newPassword string) error) {
	a.persistPassword = fn
}

// Login 校验用户名密码，成功返回 JWT token。remember=true 使用长会话。
func (a *Auth) Login(username, password string, remember bool) (string, error) {
	if username != a.username {
		return "", ErrUnauthorized
	}
	if !checkPassword(a.passHash, password) {
		return "", ErrUnauthorized
	}
	ttl := a.shortTTL
	if remember {
		ttl = a.longTTL
	}
	return a.sign(ttl)
}

// sign 签发 JWT。
func (a *Auth) sign(ttl time.Duration) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"username": a.username,
		"iat":      now.Unix(),
		"exp":      now.Add(ttl).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.secret)
}

// Verify 校验 JWT token 是否有效。返回 username。
func (a *Auth) Verify(tokenStr string) (string, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return a.secret, nil
	})
	if err != nil {
		return "", err
	}
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		if u, ok := claims["username"].(string); ok {
			return u, nil
		}
		return "", fmt.Errorf("token 缺 username")
	}
	return "", fmt.Errorf("token 无效")
}

// ChangePassword 修改密码（旧密码校验）。改密后持久化到配置文件。
// 先持久化成功再更新内存，避免写文件失败导致内存已改但重启后回退。
func (a *Auth) ChangePassword(oldPass, newPass string) error {
	if !checkPassword(a.passHash, oldPass) {
		return ErrUnauthorized
	}
	if len(newPass) < 6 {
		return &AuthError{"新密码至少 6 位"}
	}
	hash, err := hashPassword(newPass)
	if err != nil {
		return err
	}
	// 先持久化到配置文件（重启后不丢失）；失败则不更新内存，密码保持不变
	if a.persistPassword != nil {
		if err := a.persistPassword(newPass); err != nil {
			return err
		}
	}
	a.passHash = hash
	return nil
}

// ErrUnauthorized 认证失败错误。
var ErrUnauthorized = &AuthError{"用户名或密码错误"}

// AuthError 鉴权错误。
type AuthError struct{ msg string }

func (e *AuthError) Error() string { return e.msg }
