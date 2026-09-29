package panel

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/pool"
)

func TestAccountNicknameEndpoint(t *testing.T) {
	dir := t.TempDir()
	authFp := filepath.Join(dir, "workbuddy_test.json")

	a := &auth.Auth{
		UID:          "u_nick_test",
		AccessToken:  "at_test",
		RefreshToken: "rt_test",
		ExpiresAt:    9999999999,
		Nickname:     "old_name",
		FilePath:     authFp,
	}
	if err := a.SaveAtomic(); err != nil {
		t.Fatalf("setup save failed: %v", err)
	}

	p := pool.New("")
	p.Add(a)

	pan := New(Config{
		Pool:   p,
		APIKey: "secret-key",
	})

	// 1. 未鉴权请求 -> 401
	rec := httptest.NewRecorder()
	reqBody := `{"nickname":"new_name"}`
	req := httptest.NewRequest("POST", "/panel/api/accounts/u_nick_test/nickname", strings.NewReader(reqBody))
	pan.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未鉴权应返回 401，实际为 %d", rec.Code)
	}

	// 2. 账号不存在 -> 404
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/panel/api/accounts/non_existent/nickname", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer secret-key")
	pan.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("账号不存在应返回 404，实际为 %d", rec.Code)
	}

	// 3. 正常修改昵称 -> 200
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/panel/api/accounts/u_nick_test/nickname", strings.NewReader(`{"nickname":"自定义团队账号"}`))
	req.Header.Set("Authorization", "Bearer secret-key")
	pan.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("正常修改应返回 200，实际为 %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json parse: %v", err)
	}
	if resp["nickname"] != "自定义团队账号" {
		t.Fatalf("resp nickname want 自定义团队账号，got %v", resp["nickname"])
	}

	// 验证内存与磁盘均已更新
	if a.NicknameValue() != "自定义团队账号" {
		t.Fatalf("内存 nickname 未更新: %q", a.NicknameValue())
	}
	rawDisk, _ := os.ReadFile(authFp)
	parsed, _ := auth.Parse(rawDisk)
	if parsed.Nickname != "自定义团队账号" {
		t.Fatalf("磁盘 nickname 未更新: %q", parsed.Nickname)
	}

	// 4. 超长名称 (> 32 字符) -> 400
	longName := strings.Repeat("字", 33)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/panel/api/accounts/u_nick_test/nickname", bytes.NewBuffer([]byte(`{"nickname":"`+longName+`"}`)))
	req.Header.Set("Authorization", "Bearer secret-key")
	pan.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超长名称应返回 400，实际为 %d: %s", rec.Code, rec.Body.String())
	}

	// 5. 换行与控制字符过滤
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/panel/api/accounts/u_nick_test/nickname", strings.NewReader(`{"nickname":"新\n名\r\t称"}`))
	req.Header.Set("Authorization", "Bearer secret-key")
	pan.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("过滤控制字符应返回 200，实际为 %d", rec.Code)
	}
	if a.NicknameValue() != "新名称" {
		t.Fatalf("期望过滤控制字符得到 '新名称'，实际为 %q", a.NicknameValue())
	}

	// 6. 空字符串清除备注 -> 200
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/panel/api/accounts/u_nick_test/nickname", strings.NewReader(`{"nickname":""}`))
	req.Header.Set("Authorization", "Bearer secret-key")
	pan.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("空字符串清除备注应返回 200，实际为 %d", rec.Code)
	}
	if a.NicknameValue() != "" {
		t.Fatalf("清除备注后 nickname 应为空，实际为 %q", a.NicknameValue())
	}
}
