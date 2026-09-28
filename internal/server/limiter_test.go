package server

import (
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/livecfg"
)

func TestModelLimiter_MaxInFlight(t *testing.T) {
	lim := newModelLimiter()
	model := "gpt-4o"

	rel1, err1 := lim.Acquire(model, 2, 0)
	if err1 != "" || rel1 == nil {
		t.Fatalf("first acquire failed: %s", err1)
	}

	rel2, err2 := lim.Acquire(model, 2, 0)
	if err2 != "" || rel2 == nil {
		t.Fatalf("second acquire failed: %s", err2)
	}

	rel3, err3 := lim.Acquire(model, 2, 0)
	if err3 != "concurrent_limit" || rel3 != nil {
		t.Fatalf("expected concurrent_limit, got %s", err3)
	}

	// Release rel1
	rel1()
	// Releasing twice should be idempotent
	rel1()

	rel4, err4 := lim.Acquire(model, 2, 0)
	if err4 != "" || rel4 == nil {
		t.Fatalf("acquire after release failed: %s", err4)
	}

	rel2()
	rel4()
}

func TestModelLimiter_RPM(t *testing.T) {
	lim := newModelLimiter()
	model := "deepseek-chat"

	for i := 0; i < 3; i++ {
		rel, err := lim.Acquire(model, 0, 3)
		if err != "" {
			t.Fatalf("acquire %d failed: %s", i, err)
		}
		rel()
	}

	_, err := lim.Acquire(model, 0, 3)
	if err != "rpm_limit" {
		t.Fatalf("expected rpm_limit, got %s", err)
	}
}

func TestLivecfg_ValidateAuthAndFindRoute(t *testing.T) {
	snap := livecfg.Snapshot{
		APIKey: "master-key-123",
		ModelRoutes: []livecfg.ModelRoute{
			{
				ID:          "deepseek-chat",
				Target:      "tencent-code-v3",
				Fallbacks:   []string{"tencent-code-high"},
				Enabled:     true,
				Reasoning:   false,
				MaxInFlight: 2,
				RPM:         10,
			},
			{
				ID:        "deepseek-reasoner",
				Target:    "tencent-code-r1",
				Enabled:   true,
				Reasoning: true,
			},
			{
				ID:      "disabled-model",
				Target:  "tencent-code-v3",
				Enabled: false,
			},
		},
		Tokens: []livecfg.TokenConfig{
			{
				Key:     "sk-cursor-token",
				Name:    "Cursor Editor",
				Enabled: true,
				Models:  []string{"deepseek-chat"},
				RPM:     60,
			},
			{
				Key:     "sk-disabled-token",
				Name:    "Old Client",
				Enabled: false,
				Models:  []string{"*"},
			},
		},
	}

	// 1. Master key has access to any model
	valid, allowed, _ := snap.ValidateAuth("master-key-123", "deepseek-chat")
	if !valid || !allowed {
		t.Fatalf("master key should have access to deepseek-chat")
	}
	valid, allowed, _ = snap.ValidateAuth("master-key-123", "deepseek-reasoner")
	if !valid || !allowed {
		t.Fatalf("master key should have access to deepseek-reasoner")
	}

	// 2. Cursor token is only allowed deepseek-chat
	valid, allowed, name := snap.ValidateAuth("sk-cursor-token", "deepseek-chat")
	if !valid || !allowed || name != "Cursor Editor" {
		t.Fatalf("cursor token should be allowed deepseek-chat")
	}
	valid, allowed, _ = snap.ValidateAuth("sk-cursor-token", "deepseek-reasoner")
	if !valid || allowed {
		t.Fatalf("cursor token should NOT be allowed deepseek-reasoner")
	}

	// 3. Disabled token rejected
	valid, _, _ = snap.ValidateAuth("sk-disabled-token", "deepseek-chat")
	if valid {
		t.Fatalf("disabled token should be rejected")
	}

	// 4. Invalid key rejected
	valid, _, _ = snap.ValidateAuth("invalid-key", "deepseek-chat")
	if valid {
		t.Fatalf("invalid key should be rejected")
	}

	// 5. FindRoute
	r1, found1 := snap.FindRoute("deepseek-chat")
	if !found1 || r1.ID != "deepseek-chat" || r1.Target != "tencent-code-v3" || len(r1.Fallbacks) != 1 {
		t.Fatalf("FindRoute deepseek-chat failed: %+v", r1)
	}

	r2, found2 := snap.FindRoute("disabled-model")
	if !found2 || r2.Enabled {
		t.Fatalf("disabled-model should not be enabled: %+v", r2)
	}

	_, found3 := snap.FindRoute("unknown-model")
	if found3 {
		t.Fatalf("unknown-model should not be found")
	}
}
