package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

func TestProbeRequestSubSecondResponse(t *testing.T) {
	// 验证 WorkBuddy "测试连接" 轻量探测秒级响应（HTTP 200，无需上游调用）
	p := testPoolWith(&auth.Auth{UID: "u1", AccessToken: "at1", ExpiresAt: 9999999999})
	models := map[string]string{
		"deepseek-chat":     "tencent-code-v3",
		"deepseek-reasoner": "tencent-code-r1",
	}
	h := NewHandler(Config{Pool: p, Models: models, APIKey: "test-key"})

	tests := []struct {
		name     string
		body     string
		isStream bool
	}{
		{
			name:     "empty_messages_deepseek_chat",
			body:     `{"model":"deepseek-chat","messages":[]}`,
			isStream: false,
		},
		{
			name:     "empty_messages_deepseek_reasoner",
			body:     `{"model":"deepseek-reasoner","messages":[]}`,
			isStream: false,
		},
		{
			name:     "max_tokens_1_probe",
			body:     `{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}],"max_tokens":1}`,
			isStream: false,
		},
		{
			name:     "max_completion_tokens_1_probe",
			body:     `{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}],"max_completion_tokens":1}`,
			isStream: false,
		},
		{
			name:     "streaming_probe_max_tokens_1",
			body:     `{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}],"max_tokens":1,"stream":true}`,
			isStream: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer test-key")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
			}

			if tc.isStream {
				respStr := rec.Body.String()
				if !strings.Contains(respStr, "chatcmpl-probe-") || !strings.Contains(respStr, "[DONE]") {
					t.Fatalf("streaming probe response invalid: %s", respStr)
				}
			} else {
				var resp map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to parse probe JSON: %v", err)
				}
				if choices, ok := resp["choices"].([]any); !ok || len(choices) == 0 {
					t.Fatalf("probe response choices missing: %+v", resp)
				}
			}
		})
	}
}

func TestModelAliasesInModelsEndpoint(t *testing.T) {
	// 验证 /v1/models 接口能够输出配置的模型别名列表
	p := testPoolWith(&auth.Auth{UID: "u1", AccessToken: "at1", ExpiresAt: 9999999999})
	models := map[string]string{
		"deepseek-chat":     "tencent-code-v3",
		"deepseek-reasoner": "tencent-code-r1",
	}
	up := upstream.New()
	h := NewHandler(Config{Pool: p, Upstream: up, Models: models, APIKey: "test-key"})

	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var resp struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	foundChat := false
	foundReasoner := false
	for _, m := range resp.Data {
		id, _ := m["id"].(string)
		if id == "deepseek-chat" {
			foundChat = true
		}
		if id == "deepseek-reasoner" {
			foundReasoner = true
			if supp, _ := m["supports_reasoning"].(bool); !supp {
				t.Errorf("deepseek-reasoner should have supports_reasoning=true")
			}
		}
	}

	if !foundChat {
		t.Errorf("deepseek-chat alias not found in /v1/models output")
	}
	if !foundReasoner {
		t.Errorf("deepseek-reasoner alias not found in /v1/models output")
	}
}

func TestModelAliasRoutingAndThinkingInjection(t *testing.T) {
	// 验证请求 deepseek-chat 被重写映射到 tencent-code-v3；
	// 请求 deepseek-reasoner 被重写映射到 tencent-code-r1 且注入 thinking
	var receivedBody []byte
	up := upstream.New()
	up.ChatHTTP = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			b, _ := io.ReadAll(req.Body)
			receivedBody = b
			// 返回上游 mock 流式响应
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: io.NopCloser(strings.NewReader(
					"data: {\"id\":\"chat1\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n" +
						"data: [DONE]\n\n",
				)),
			}, nil
		}),
	}

	p := testPoolWith(&auth.Auth{UID: "u1", AccessToken: "at1", ExpiresAt: 9999999999})
	models := map[string]string{
		"deepseek-chat":     "tencent-code-v3",
		"deepseek-reasoner": "tencent-code-r1",
	}
	h := NewHandler(Config{Pool: p, Upstream: up, Models: models, APIKey: "test-key"})

	// 1. 测试 deepseek-chat 路由
	chatReq := `{"model":"deepseek-chat","messages":[{"role":"user","content":"编写一段代码"}],"max_tokens":100}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(chatReq))
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("chat request failed: %d body=%s", rec.Code, rec.Body.String())
	}

	var parsedChat map[string]any
	json.Unmarshal(receivedBody, &parsedChat)
	if parsedChat["model"] != "tencent-code-v3" {
		t.Errorf("expected model tencent-code-v3, got %v", parsedChat["model"])
	}

	// 2. 测试 deepseek-reasoner 路由与 thinking 注入
	receivedBody = nil
	reasonReq := `{"model":"deepseek-reasoner","messages":[{"role":"user","content":"推导数学公式"}],"max_tokens":100}`
	req2 := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(reasonReq))
	req2.Header.Set("Authorization", "Bearer test-key")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("reasoner request failed: %d body=%s", rec2.Code, rec2.Body.String())
	}

	var parsedReason map[string]any
	json.Unmarshal(receivedBody, &parsedReason)
	if parsedReason["model"] != "tencent-code-r1" {
		t.Errorf("expected model tencent-code-r1, got %v", parsedReason["model"])
	}

	th, ok := parsedReason["thinking"].(map[string]any)
	if !ok || th["type"] != "enabled" {
		t.Errorf("expected thinking:{type:enabled}, got %+v", parsedReason["thinking"])
	}
}

func TestRootRedirectsToPanel(t *testing.T) {
	dummyPanel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("panel-ok"))
	})
	h := NewHandler(Config{
		Panel: dummyPanel,
	})

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302 Found, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/panel/" {
		t.Fatalf("expected Location /panel/, got %q", loc)
	}
}

