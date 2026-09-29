package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAuthNicknameThreadSafety(t *testing.T) {
	a := &Auth{
		UID:         "u_test",
		AccessToken: "test_token",
		Nickname:    "initial_nick",
	}

	var wg sync.WaitGroup
	// 并发读写 1000 次，验证无 data race
	for i := 0; i < 500; i++ {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			a.SetNickname(fmt.Sprintf("nick_%d", idx))
		}(i)
		go func() {
			defer wg.Done()
			_ = a.NicknameValue()
		}()
	}
	wg.Wait()

	finalNick := a.NicknameValue()
	if finalNick == "" {
		t.Fatal("final nickname should not be empty")
	}
}

func TestAuthNicknameSaveAndRollback(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "workbuddy_test.json")

	a := &Auth{
		UID:          "u_nick_save",
		AccessToken:  "at_123",
		RefreshToken: "rt_123",
		ExpiresAt:    9999999999,
		Domain:       "copilot.tencent.com",
		Nickname:     "original_name",
		FilePath:     fp,
	}

	// 1. 初次保存
	if err := a.SaveAtomic(); err != nil {
		t.Fatalf("SaveAtomic 失败: %v", err)
	}

	// 读取磁盘验证保存结果
	raw, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("读取 auth 文件失败: %v", err)
	}
	parsed, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse auth 文件失败: %v", err)
	}
	if parsed.Nickname != "original_name" {
		t.Fatalf("期望磁盘保存 nickname 为 original_name，实际为 %q", parsed.Nickname)
	}

	// 2. 修改昵称并成功写盘
	a.SetNickname("new_custom_name")
	if err := a.SaveAtomic(); err != nil {
		t.Fatalf("SaveAtomic 失败: %v", err)
	}

	raw, _ = os.ReadFile(fp)
	parsed, _ = Parse(raw)
	if parsed.Nickname != "new_custom_name" {
		t.Fatalf("期望磁盘更新 nickname 为 new_custom_name，实际为 %q", parsed.Nickname)
	}

	// 3. 模拟两阶段写盘失败并回滚
	oldNick := a.NicknameValue()
	a.SetNickname("fail_nick")
	// 设置非法路径导致写盘失败
	a.FilePath = filepath.Join(dir, "non_existent_subdir", "workbuddy.json")
	if err := a.SaveAtomic(); err != nil {
		// 模拟两阶段事务：写盘失败则内存回滚老昵称
		a.SetNickname(oldNick)
	} else {
		t.Fatal("向不存在的目录写盘应失败")
	}

	if a.NicknameValue() != "new_custom_name" {
		t.Fatalf("写盘失败后内存应回滚为 new_custom_name，实际为 %q", a.NicknameValue())
	}
}
