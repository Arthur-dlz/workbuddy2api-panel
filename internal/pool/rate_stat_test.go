package pool

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
)

// TestRateStatRecordOn6004：CooldownSoftForModel（resetAt 非零）两次 →
// Total=2、Count24h=2、LastResetAt==resetAt；resetAt 零值分支不记录。
func TestRateStatRecordOn6004(t *testing.T) {
	p := New("")
	p.Add(&auth.Auth{UID: "u1"})
	reset := time.Now().Add(10 * time.Minute)
	p.CooldownSoftForModel("u1", 30*time.Second, reset, "m1", "6004")
	p.CooldownSoftForModel("u1", 30*time.Second, reset, "m1", "6004")

	p.mu.RLock()
	st, ok := p.byUID["u1"].modelRateStats["m1"]
	p.mu.RUnlock()
	if !ok {
		t.Fatalf("m1 rate stat not recorded")
	}
	if st.Total != 2 || st.Count24h != 2 {
		t.Errorf("Total=%d Count24h=%d want 2/2", st.Total, st.Count24h)
	}
	if !st.LastResetAt.Equal(reset) {
		t.Errorf("LastResetAt=%v want %v", st.LastResetAt, reset)
	}

	// resetAt 零值分支（账号级软冷却）：不记录台账。
	p.CooldownSoftForModel("u1", 30*time.Second, time.Time{}, "m2", "429")
	p.mu.RLock()
	_, ok2 := p.byUID["u1"].modelRateStats["m2"]
	p.mu.RUnlock()
	if ok2 {
		t.Errorf("resetAt==0 must NOT record rate stat")
	}
}

// TestRateStatWindowRoll：把 WindowStart 手工拨到 25h 前再触发 →
// Count24h 重置为 1、Total 继续累加。
func TestRateStatWindowRoll(t *testing.T) {
	p := New("")
	p.Add(&auth.Auth{UID: "u1"})
	reset := time.Now().Add(10 * time.Minute)
	p.CooldownSoftForModel("u1", 30*time.Second, reset, "m1", "6004")
	p.CooldownSoftForModel("u1", 30*time.Second, reset, "m1", "6004")

	// 手工把窗口起点拨到 25h 前（模拟窗口过期）。
	p.mu.Lock()
	st := p.byUID["u1"].modelRateStats["m1"]
	st.WindowStart = time.Now().Add(-25 * time.Hour)
	p.byUID["u1"].modelRateStats["m1"] = st
	p.mu.Unlock()

	// 再触发一次：窗口应滚动 → Count24h=1，Total 累加到 3。
	p.CooldownSoftForModel("u1", 30*time.Second, reset, "m1", "6004")
	p.mu.RLock()
	st2 := p.byUID["u1"].modelRateStats["m1"]
	p.mu.RUnlock()
	if st2.Count24h != 1 {
		t.Errorf("Count24h=%d want 1 (window rolled)", st2.Count24h)
	}
	if st2.Total != 3 {
		t.Errorf("Total=%d want 3 (accumulates)", st2.Total)
	}
}

// TestPickRateStatPenalty：同模型下，24h 内 429 过 1 次的账号 vs 干净账号，
// 确定性随机源下干净账号必中；换另一个模型请求，两者恢复同台竞争。
func TestPickRateStatPenalty(t *testing.T) {
	withNoPickGap(t)
	p := New("")
	p.SetPreferExpiring(false)
	// 恒取首个（r=0）：受罚账号若排在首位也不会被抽中——证明惩罚生效于权重。
	p.SetRandomSource(func(n int64) int64 { return 0 })
	p.Add(&auth.Auth{UID: "dirty"})
	p.Add(&auth.Auth{UID: "clean"})
	p.SetCredits("dirty", 100, 0)
	p.SetCredits("clean", 100, 0)

	// dirty 在模型 m 上 429 过 1 次（非零 resetAt）。
	p.CooldownSoftForModel("dirty", 30*time.Second, time.Now().Add(10*time.Minute), "m", "6004")

	// 请求模型 m：clean 必中 20 次（dirty 权重被除数 2 拉低，且排序后 clean 在前）。
	for i := 0; i < 20; i++ {
		got := p.PickForModel("m")
		if got == nil || got.UID != "clean" {
			t.Fatalf("iter %d: pick=%v want clean (dirty penalized on model m)", i, got)
		}
	}
	// 请求另一个模型 m2：无台账惩罚，两者恢复同台竞争。
	// 先对齐两者 lastUsed（上面的 pick 循环只更新了 clean 的 lastUsed，会让
	// idle 补偿产生差异），再比对权重——此时差异只可能来自 429 惩罚。
	ts := time.Now()
	p.mu.Lock()
	p.byUID["dirty"].lastUsed = ts
	p.byUID["clean"].lastUsed = ts
	p.mu.Unlock()
	now := time.Now()
	wd := p.weightOf(p.byUID["dirty"], 100, now, "m2")
	wc := p.weightOf(p.byUID["clean"], 100, now, "m2")
	if wd != wc {
		t.Errorf("on model m2: dirty weight=%v clean weight=%v want equal (no penalty)", wd, wc)
	}
	// 而 m 上 dirty 权重应小于 clean。
	wdm := p.weightOf(p.byUID["dirty"], 100, now, "m")
	wcm := p.weightOf(p.byUID["clean"], 100, now, "m")
	if !(wdm < wcm) {
		t.Errorf("on model m: dirty weight=%v should be < clean weight=%v", wdm, wcm)
	}
}

// TestRateStatPersistRoundTrip：save→load 后 Total/Count24h/时刻保留；
// LastAt 8 天前的条目被过滤。
func TestRateStatPersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "state.json")

	p := New(fp)
	p.Add(&auth.Auth{UID: "u1"})
	reset := time.Now().Add(15 * time.Minute)
	p.CooldownSoftForModel("u1", 30*time.Second, reset, "m1", "6004")
	p.CooldownSoftForModel("u1", 30*time.Second, reset, "m1", "6004")
	// 再加一个 8 天前的陈旧条目（应被过滤）。
	p.mu.Lock()
	p.byUID["u1"].modelRateStats["old"] = modelRateStat{
		Total: 5, Count24h: 5,
		WindowStart: time.Now().Add(-9 * 24 * time.Hour),
		LastAt:      time.Now().Add(-8 * 24 * time.Hour),
		LastResetAt: time.Now().Add(-8 * 24 * time.Hour),
	}
	p.mu.Unlock()
	p.Flush() // 落盘

	// 新池从同文件恢复。
	p2 := New(fp)
	p2.mu.RLock()
	st, ok := p2.byUID["u1"].modelRateStats["m1"]
	_, hasOld := p2.byUID["u1"].modelRateStats["old"]
	p2.mu.RUnlock()
	if !ok {
		t.Fatalf("m1 rate stat lost after reload")
	}
	if st.Total != 2 || st.Count24h != 2 {
		t.Errorf("after reload Total=%d Count24h=%d want 2/2", st.Total, st.Count24h)
	}
	if hasOld {
		t.Errorf("stale (LastAt 8d ago) entry must be filtered on reload")
	}
}
