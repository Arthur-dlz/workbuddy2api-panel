package panel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/livecfg"
)

func TestPanelUpstreamModelsEndpoint(t *testing.T) {
	live := livecfg.New(livecfg.Snapshot{
		APIKey: "sk-test",
		ModelRoutes: []livecfg.ModelRoute{
			{
				ID:     "custom-alias",
				Target: "deepseek-v4-pro",
			},
		},
	})

	p := New(Config{
		APIKey: "sk-test",
		Live:   live,
	})

	// 测试需要鉴权
	reqUnauthorized := httptest.NewRequest("GET", "/panel/api/upstream-models", nil)
	rrUnauthorized := httptest.NewRecorder()
	p.mux.ServeHTTP(rrUnauthorized, reqUnauthorized)
	if rrUnauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", rrUnauthorized.Code)
	}

	// 测试携带正确密钥但没有账号池时返回 503
	reqAuth := httptest.NewRequest("GET", "/panel/api/upstream-models", nil)
	reqAuth.Header.Set("Authorization", "Bearer sk-test")
	rrAuth := httptest.NewRecorder()
	p.mux.ServeHTTP(rrAuth, reqAuth)

	if rrAuth.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without accounts, got %d", rrAuth.Code)
	}
	var errResp map[string]any
	if err := json.Unmarshal(rrAuth.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errResp["ok"] == true {
		t.Fatalf("expected ok=false")
	}
}
