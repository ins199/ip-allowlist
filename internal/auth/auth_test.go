package auth

import (
	"testing"
	"time"
)

func newTestAuth(t *testing.T) *Auth {
	t.Helper()
	a, err := New("admin", "secret123", "test-secret-key-0123456789abcdef", time.Hour, 24*time.Hour)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	return a
}

func TestLoginVerify(t *testing.T) {
	a := newTestAuth(t)
	token, err := a.Login("admin", "secret123", false)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	username, err := a.Verify(token)
	if err != nil || username != "admin" {
		t.Fatalf("Verify 失败: username=%q err=%v", username, err)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	a := newTestAuth(t)
	if _, err := a.Login("admin", "wrong", false); err == nil {
		t.Fatal("错误密码应登录失败")
	}
}

func TestLoginWrongUsername(t *testing.T) {
	a := newTestAuth(t)
	if _, err := a.Login("nobody", "secret123", false); err == nil {
		t.Fatal("错误用户名应登录失败")
	}
}

func TestVerifyInvalidToken(t *testing.T) {
	a := newTestAuth(t)
	if _, err := a.Verify("invalid.token.here"); err == nil {
		t.Fatal("无效 token 应校验失败")
	}
	// 用不同 secret 签发的 token 应校验失败
	b, _ := New("admin", "x", "other-secret-key-0987654321fedcba", time.Hour, time.Hour)
	token, _ := b.Login("admin", "x", false)
	if _, err := a.Verify(token); err == nil {
		t.Fatal("不同 secret 的 token 应校验失败")
	}
}

func TestRememberLongTTL(t *testing.T) {
	a := newTestAuth(t)
	short, _ := a.Login("admin", "secret123", false)
	long, _ := a.Login("admin", "secret123", true)
	// 短会话和长会话 token 应不同（TTL 不同）
	if short == long {
		t.Fatal("remember 不同应生成不同 token")
	}
	// 过期 token 应校验失败（负 TTL 签发）
	expired, _ := a.sign(-time.Minute)
	if _, err := a.Verify(expired); err == nil {
		t.Fatal("过期 token 应校验失败")
	}
}

func TestChangePassword(t *testing.T) {
	a := newTestAuth(t)
	// 持久化回调（记录新密码）
	var persisted string
	a.SetPersistPassword(func(np string) error { persisted = np; return nil })

	if err := a.ChangePassword("secret123", "newpass456"); err != nil {
		t.Fatalf("改密失败: %v", err)
	}
	if persisted != "newpass456" {
		t.Fatalf("持久化回调未调用或值不对: %q", persisted)
	}
	// 新密码可登录，旧密码不可
	if _, err := a.Login("admin", "newpass456", false); err != nil {
		t.Fatalf("新密码应可登录: %v", err)
	}
	if _, err := a.Login("admin", "secret123", false); err == nil {
		t.Fatal("旧密码不应再可登录")
	}
}

func TestChangePasswordWrongOld(t *testing.T) {
	a := newTestAuth(t)
	if err := a.ChangePassword("wrong-old", "newpass456"); err == nil {
		t.Fatal("旧密码错误应改密失败")
	}
}

func TestChangePasswordTooShort(t *testing.T) {
	a := newTestAuth(t)
	if err := a.ChangePassword("secret123", "123"); err == nil {
		t.Fatal("新密码过短应失败")
	}
}

func TestNewEmptySecret(t *testing.T) {
	if _, err := New("admin", "x", "", time.Hour, time.Hour); err == nil {
		t.Fatal("空 secret 应报错")
	}
}
