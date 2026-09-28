package panel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/livecfg"
)

func TestPanelModelsWithAliases(t *testing.T) {
	live := livecfg.New(livecfg.Snapshot{
		APIKey: "test-key",
		Models: map[string]string{
			"deepseek-chat":     "tencent-code-v3",
			"deepseek-reasoner": "tencent-code-r1",
		},
	})

	p := New(Config{
		Version: "test",
		APIKey:  "test-key",
		Live:    live,
	})

	req := httptest.NewRequest("GET", "/panel/api/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		OK     bool             `json:"ok"`
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !resp.OK {
		t.Fatalf("expected ok: true")
	}
	if len(resp.Models) != 2 {
		t.Fatalf("expected 2 alias models, got %d", len(resp.Models))
	}

	m0 := resp.Models[0]
	if m0["id"] != "deepseek-chat" || m0["is_alias"] != true {
		t.Errorf("model 0 mismatch: %+v", m0)
	}
	m1 := resp.Models[1]
	if m1["id"] != "deepseek-reasoner" || m1["is_alias"] != true || m1["supports_reasoning"] != true {
		t.Errorf("model 1 mismatch: %+v", m1)
	}
}
