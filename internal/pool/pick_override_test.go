package pool

import (
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
)

// ptrF 返回 *float64（构造 AccountOverride.Weight 用）。
func ptrF(v float64) *float64 { return &v }

// TestPickPriorityDefaultNoop（特殊工作流：先写、先跑、记录序列，再做 P1 改动，断言序列不变）。
// 默认配置（无 account override）下，选号序列必须与改造前逐字一致 —— 证明 P1 的
// priority 桶过滤 / weight 乘子在零配置下是完全 no-op（零回归）。
//
// 建池骨架照抄 TestPickDeterministicViaSetRandomSource：恒定随机源 r=0。
// 刻意的确定性设计：随机源恒返回 0（而非 call++%n）——因为 pickWeighted 的权重含
// 「闲置补偿」，该因子依赖 time.Now()，用轮转源会让序列带上时间抖动（非确定）。
// 恒 0 源下 r=0 恒命中候选序列首个（权重降序 → credits 最高者），完全时间无关。
// 三个账号同 credits → 权重并列，稳定 tie-break（UID 序）同样确定。
//
// want 是「P1 改动前」实测记录的确定性输出；P1 后必须逐字不变。
func TestPickPriorityDefaultNoop(t *testing.T) {
	withNoPickGap(t)
	p := New("")
	p.SetRandomSource(func(n int64) int64 { return 0 })
	p.Add(&auth.Auth{UID: "u1"})
	p.Add(&auth.Auth{UID: "u2"})
	p.Add(&auth.Auth{UID: "u3"})
	p.SetCredits("u1", 100, 0)
	p.SetCredits("u2", 50, 0)
	p.SetCredits("u3", 10, 0)

	var seq []string
	for i := 0; i < 30; i++ {
		got := p.Pick()
		if got == nil {
			t.Fatalf("iter %d: pick=nil", i)
		}
		seq = append(seq, got.UID)
	}
	// 期望序列（P1 改动前记录的确定性输出；P1 后必须逐字不变）。
	want := []string{
		"u1", "u1", "u1", "u1", "u1", "u1", "u1", "u1", "u1", "u1",
		"u1", "u1", "u1", "u1", "u1", "u1", "u1", "u1", "u1", "u1",
		"u1", "u1", "u1", "u1", "u1", "u1", "u1", "u1", "u1", "u1",
	}
	if len(seq) != len(want) {
		t.Fatalf("seq len=%d want %d", len(seq), len(want))
	}
	for i := range want {
		if seq[i] != want[i] {
			t.Fatalf("iter %d: got %q want %q\nfull seq=%v", i, seq[i], want[i], seq)
		}
	}
}

// TestPickPriorityBucketFilter：3 账号（两个默认 priority=0、一个 priority=1），
// priority 桶过滤后只有 priority=1 的账号进入候选，连续 pick 20 次全部命中它。
// 确定性随机源 r=0（恒取首候选）——桶过滤保证了候选集只含高优先级号。
func TestPickPriorityBucketFilter(t *testing.T) {
	withNoPickGap(t)
	p := New("")
	p.SetRandomSource(func(n int64) int64 { return 0 })
	p.Add(&auth.Auth{UID: "low1"})
	p.Add(&auth.Auth{UID: "low2"})
	p.Add(&auth.Auth{UID: "high"})
	p.SetCredits("low1", 100, 0)
	p.SetCredits("low2", 100, 0)
	p.SetCredits("high", 100, 0)
	// high priority=1，两个 low 保持默认 0。
	p.SetAccountOverrides(map[string]AccountOverride{
		"high": {Priority: 1},
	})
	for i := 0; i < 20; i++ {
		got := p.Pick()
		if got == nil || got.UID != "high" {
			t.Fatalf("iter %d: pick=%v want high (priority bucket)", i, got)
		}
	}
}

// TestWeightZeroNeverDrawn：候选集混合（一个 weight 显式 0、其余为正）——
// 刻意避免"全部 weight=0 → pickWeighted 的 total<=0 均匀随机兜底"边界。
// SetPreferExpiring(false) 关掉到期优先分支，确保走加权抽签路径（weight 生效处）。
// pick 50 次，weight=0 账号 0 次被抽中。
func TestWeightZeroNeverDrawn(t *testing.T) {
	withNoPickGap(t)
	p := New("")
	p.SetPreferExpiring(false)
	// 递增随机源：覆盖各候选（否则恒取首个可能恒命中/恒不命中，断言不充分）。
	var call int64
	p.SetRandomSource(func(n int64) int64 {
		call++
		return call % n
	})
	p.Add(&auth.Auth{UID: "zero"})
	p.Add(&auth.Auth{UID: "pos1"})
	p.Add(&auth.Auth{UID: "pos2"})
	for _, u := range []string{"zero", "pos1", "pos2"} {
		p.SetCredits(u, 100, 0)
	}
	// zero 显式 0（恒不抽中）；pos1/pos2 缺省（weight=1）。
	p.SetAccountOverrides(map[string]AccountOverride{
		"zero": {Weight: ptrF(0)},
	})
	for i := 0; i < 50; i++ {
		got := p.Pick()
		if got == nil {
			t.Fatalf("iter %d: pick=nil", i)
		}
		if got.UID == "zero" {
			t.Fatalf("iter %d: weight=0 account must never be drawn, got %q", i, got.UID)
		}
	}
}

// TestSetAccountOverridesSemantics：nil→1、显式 0→0、负→钳 0、删除 override 后复位 0/1。
// 直接读 entry 生效值（包内测试可访问）。
func TestSetAccountOverridesSemantics(t *testing.T) {
	p := New("")
	p.Add(&auth.Auth{UID: "a"})
	p.Add(&auth.Auth{UID: "b"})
	p.Add(&auth.Auth{UID: "c"})

	p.SetAccountOverrides(map[string]AccountOverride{
		"a": {Priority: 5, Weight: ptrF(0)}, // 显式 0
		"b": {Priority: 2, Weight: ptrF(-3)}, // 负 → 钳 0
		"c": {Priority: 1},                   // nil → 1
	})
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.byUID["a"].priority != 5 || p.byUID["a"].weight != 0 {
		t.Errorf("a: priority=%d weight=%v want 5/0", p.byUID["a"].priority, p.byUID["a"].weight)
	}
	if p.byUID["b"].priority != 2 || p.byUID["b"].weight != 0 {
		t.Errorf("b: priority=%d weight=%v want 2/0 (负钳 0)", p.byUID["b"].priority, p.byUID["b"].weight)
	}
	if p.byUID["c"].priority != 1 || p.byUID["c"].weight != 1 {
		t.Errorf("c: priority=%d weight=%v want 1/1 (nil→1)", p.byUID["c"].priority, p.byUID["c"].weight)
	}
}

// TestSetAccountOverridesDeletionResets：删除 override 后（重新注入空/不含该 uid 的表），
// 该 entry 的 priority/weight 必须复位为 0/1（"先全量复位再应用"契约）。
func TestSetAccountOverridesDeletionResets(t *testing.T) {
	p := New("")
	p.Add(&auth.Auth{UID: "a"})
	p.SetAccountOverrides(map[string]AccountOverride{
		"a": {Priority: 9, Weight: ptrF(0)},
	})
	// 重新注入空表 → a 复位。
	p.SetAccountOverrides(nil)
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.byUID["a"].priority != 0 || p.byUID["a"].weight != 1 {
		t.Errorf("after delete: priority=%d weight=%v want 0/1", p.byUID["a"].priority, p.byUID["a"].weight)
	}
}
