package pool

import (
	"testing"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
)

// TestEarliestRecovery 覆盖池内最早恢复时刻的四种情形：
// 冷却账号返回其 until；无冷却 → false；realm 过滤生效；disabled 不计。
func TestEarliestRecovery(t *testing.T) {
	p := New("")
	p.Add(&auth.Auth{UID: "cn1", Domain: "www.codebuddy.cn"})
	p.Add(&auth.Auth{UID: "gl1", Domain: "www.workbuddy.ai"})

	// 无冷却 → false。
	if _, ok := p.EarliestRecovery(""); ok {
		t.Fatalf("no cooling accounts must return false")
	}

	// 账号级冷却：cn1 冷却至 now+1m。
	p.Cooldown("cn1", CoolSoft, 1*time.Minute, "test")
	rt, ok := p.EarliestRecovery("")
	if !ok {
		t.Fatalf("want ok=true after cooldown")
	}
	if d := time.Until(rt); d <= 30*time.Second || d > 70*time.Second {
		t.Errorf("earliest recovery ≈1m, got %v", d)
	}

	// realm 过滤：查 global 域时 cn1 的冷却不计入 → false。
	if _, ok := p.EarliestRecovery("global"); ok {
		t.Errorf("realm filter must exclude cn1's cooldown")
	}

	// 模型级冷却：gl1 在模型 m 上 6004 冷却至 now+2m，其 Until 计入。
	p.CooldownSoftForModel("gl1", 30*time.Second, time.Now().Add(2*time.Minute), "m", "6004")
	rt2, ok := p.EarliestRecovery("")
	if !ok {
		t.Fatalf("want ok=true with model cooldown")
	}
	// cn1 的 ~1m 早于 gl1 的 ~2m → best 仍应是 cn1 的冷 cooldown（最早者）。
	if d := time.Until(rt2); d > 90*time.Second {
		t.Errorf("earliest should be cn1's ~1m, got %v", d)
	}

	// disabled 不计：禁用 cn1 后，跨域查询只剩 gl1 的 ~2m。
	p.Disable("cn1", "test disable")
	rt3, ok := p.EarliestRecovery("")
	if !ok {
		t.Fatalf("want ok=true (gl1 model cooldown remains)")
	}
	if d := time.Until(rt3); d < 90*time.Second {
		t.Errorf("disabled account must be excluded; earliest should be gl1's ~2m, got %v", d)
	}
}

// TestEarliestRecoveryRealmModelCooldown 模型级冷却按 realm 过滤：
// 只查 cn 域时不统计 global 账号的模型冷却。
func TestEarliestRecoveryRealmModelCooldown(t *testing.T) {
	p := New("")
	p.Add(&auth.Auth{UID: "gl1", Domain: "www.workbuddy.ai"})
	p.CooldownSoftForModel("gl1", 30*time.Second, time.Now().Add(3*time.Minute), "m", "6004")
	if _, ok := p.EarliestRecovery("cn"); ok {
		t.Errorf("cn realm must not see global account's model cooldown")
	}
	if _, ok := p.EarliestRecovery("global"); !ok {
		t.Errorf("global realm must see its model cooldown")
	}
}
