package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/livecfg"
	"github.com/linguo2625469/workbuddy2api-panel/internal/pool"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

func TestTokenBindModelOverride(t *testing.T) {
	p := pool.New("")
	live := livecfg.New(livecfg.Snapshot{
		APIKey: "sk-master",
		Tokens: []livecfg.TokenConfig{
			{
				Key:       "sk-deepseek-bound",
				Name:      "DeepSeek-Dedicated",
				Enabled:   true,
				BindModel: "deepseek-v4-pro",
			},
			{
				Key:       "sk-kimi-bound",
				Name:      "Kimi-Dedicated",
				Enabled:   true,
				BindModel: "kimi-k3-1",
			},
		},
	})

	h := NewHandler(Config{
		Pool:     p,
		Upstream: upstream.New(),
		APIKey:   "sk-master",
		Live:     live,
	})

	// 1. 测试 models 接口针对绑定 Token 返回的内容
	reqModels := httptest.NewRequest("GET", "/v1/models", nil)
	reqModels.Header.Set("Authorization", "Bearer sk-deepseek-bound")
	rrModels := httptest.NewRecorder()
	h.models(rrModels, reqModels)

	if rrModels.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rrModels.Code)
	}

	var mResp struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rrModels.Body.Bytes(), &mResp); err != nil {
		t.Fatalf("unmarshal models: %v", err)
	}
	if len(mResp.Data) == 0 {
		t.Fatalf("expected non-empty models")
	}
	// 首位必须是绑定的模型
	firstID := mResp.Data[0]["id"]
	if firstID != "deepseek-v4-pro" {
		t.Errorf("expected first model to be deepseek-v4-pro, got %v", firstID)
	}
	if isDef, _ := mResp.Data[0]["is_default"].(bool); !isDef {
		t.Errorf("expected is_default=true on bound model")
	}

	// 2. 测试探测请求在绑定 Token 下秒级响应并识别绑定模型
	probeBody := []byte(`{"messages":[{"role":"user","content":"test"}],"max_tokens":1,"stream":false,"model":"default"}`)
	reqProbe := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(probeBody))
	reqProbe.Header.Set("Authorization", "Bearer sk-deepseek-bound")
	rrProbe := httptest.NewRecorder()
	h.chatCompletions(rrProbe, reqProbe)

	if rrProbe.Code != http.StatusOK {
		t.Fatalf("expected 200 for probe request, got %d (body: %s)", rrProbe.Code, rrProbe.Body.String())
	}
	var probeResp map[string]any
	if err := json.Unmarshal(rrProbe.Body.Bytes(), &probeResp); err != nil {
		t.Fatalf("unmarshal probe resp: %v", err)
	}
	// 探测响应中的模型字段应为绑定的真实模型
	if probeResp["model"] != "deepseek-v4-pro" {
		t.Errorf("expected probe response model deepseek-v4-pro, got %v", probeResp["model"])
	}
}
