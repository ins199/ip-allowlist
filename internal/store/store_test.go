package store

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "test.json"))
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	return s
}

func TestUpsertAndGetRule(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertRule(PortRule{Port: 22, Comment: "SSH", Strict: true}); err != nil {
		t.Fatalf("UpsertRule 失败: %v", err)
	}
	r := s.GetRule(22)
	if r == nil || r.Comment != "SSH" || !r.Strict {
		t.Fatalf("GetRule 结果不符: %+v", r)
	}
	// 更新已存在规则
	if err := s.UpsertRule(PortRule{Port: 22, Comment: "SSH-Update", Strict: false}); err != nil {
		t.Fatalf("UpsertRule 更新失败: %v", err)
	}
	r = s.GetRule(22)
	if r.Comment != "SSH-Update" || r.Strict {
		t.Fatalf("更新后结果不符: %+v", r)
	}
}

func TestAddIPDedupAndDel(t *testing.T) {
	s := newTestStore(t)
	_ = s.UpsertRule(PortRule{Port: 22, Comment: "SSH"})

	added, err := s.AddIP(22, "1.2.3.4", "备注")
	if err != nil || !added {
		t.Fatalf("AddIP 首次应新增: added=%v err=%v", added, err)
	}
	// 重复添加应去重
	added, err = s.AddIP(22, "1.2.3.4", "备注")
	if err != nil || added {
		t.Fatalf("AddIP 重复应去重: added=%v err=%v", added, err)
	}
	// 删除
	deleted, err := s.DelIP(22, "1.2.3.4")
	if err != nil || !deleted {
		t.Fatalf("DelIP 应删除: deleted=%v err=%v", deleted, err)
	}
	r := s.GetRule(22)
	if len(r.AllowList) != 0 {
		t.Fatalf("删除后白名单应为空: %+v", r.AllowList)
	}
}

func TestSetStrict(t *testing.T) {
	s := newTestStore(t)
	_ = s.UpsertRule(PortRule{Port: 22, Comment: "SSH", Strict: false})
	if err := s.SetStrict(22, true); err != nil {
		t.Fatalf("SetStrict 失败: %v", err)
	}
	if r := s.GetRule(22); !r.Strict {
		t.Fatalf("SetStrict 后应为严格模式")
	}
}

func TestPersistAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	s, err := New(path)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	_ = s.UpsertRule(PortRule{Port: 22, Comment: "SSH", Strict: true})
	_, _ = s.AddIP(22, "1.2.3.4", "备注")
	_ = s.AddLoginLog(LoginLog{Time: "t", IP: "1.2.3.4", Success: true, Username: "admin"})

	// 重新加载
	s2, err := New(path)
	if err != nil {
		t.Fatalf("重新 New 失败: %v", err)
	}
	r := s2.GetRule(22)
	if r == nil || r.Comment != "SSH" || !r.Strict || len(r.AllowList) != 1 {
		t.Fatalf("重载后规则不符: %+v", r)
	}
	if len(s2.GetLoginLogs()) != 1 {
		t.Fatalf("重载后登录记录应为 1 条")
	}
}

func TestLoadCorruptRecoverFromBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	s, _ := New(path)
	_ = s.UpsertRule(PortRule{Port: 22, Comment: "SSH"})
	// 保存后 .bak 应存在（保存前把旧文件 rename 为 .bak）
	_ = s.UpsertRule(PortRule{Port: 22, Comment: "SSH-V2"})
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("保存后 .bak 应存在: %v", err)
	}

	// 损坏主文件，应能自动从 .bak 恢复（.bak 为上一次成功版本 "SSH"）
	if err := os.WriteFile(path, []byte("{corrupted"), 0644); err != nil {
		t.Fatal(err)
	}
	s2, err := New(path)
	if err != nil {
		t.Fatalf("损坏后应从 .bak 恢复而非报错: %v", err)
	}
	if r := s2.GetRule(22); r == nil || r.Comment != "SSH" {
		t.Fatalf("恢复后应为上一次版本 SSH: %+v", r)
	}
}

func TestLoginLogCap(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < MaxLoginLogs+10; i++ {
		_ = s.AddLoginLog(LoginLog{Time: "t", IP: "1.2.3.4", Success: true, Username: "admin"})
	}
	if n := len(s.GetLoginLogs()); n != MaxLoginLogs {
		t.Fatalf("登录记录应保留 %d 条，实际 %d", MaxLoginLogs, n)
	}
}

func TestDeleteRule(t *testing.T) {
	s := newTestStore(t)
	_ = s.UpsertRule(PortRule{Port: 22, Comment: "SSH"})
	_ = s.UpsertRule(PortRule{Port: 6379, Comment: "Redis"})
	if err := s.DeleteRule(22); err != nil {
		t.Fatalf("DeleteRule 失败: %v", err)
	}
	if s.GetRule(22) != nil {
		t.Fatalf("删除后 22 应不存在")
	}
	if s.GetRule(6379) == nil {
		t.Fatalf("6379 应保留")
	}
}
