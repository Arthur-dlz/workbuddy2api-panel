package usage

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 成功/失败尝试计数、total 的 pt+ct 兜底口径、按域/账号聚合。
func TestAddAndTotals(t *testing.T) {
	r := New("")
	now := time.Now()
	r.Add(now, "cn", "uid1", "glm-5.2", "master", Delta{PromptTokens: 100, HasPromptTokens: true, CompletionTokens: 50, HasCompletion: true, LatencyMs: 200, HasLatency: true}, true)
	// 失败尝试：无 usage → 只计请求数与失败数，token 不加。
	r.Add(now, "global", "uid1", "claude-4.6", "master", Delta{}, false)
	// 上游没给 total 时用 pt+ct 兜底，保证总量口径连续。
	r.Add(now, "cn", "uid1", "glm-5.2", "master", Delta{PromptTokens: 10, HasPromptTokens: true, CompletionTokens: 5, HasCompletion: true}, true)

	s := r.Snapshot(24, nil)
	if s.Totals.Requests != 3 || s.Totals.Errors != 1 {
		t.Fatalf("requests/errors = %d/%d, want 3/1", s.Totals.Requests, s.Totals.Errors)
	}
	if s.Totals.PromptTokens != 110 || s.Totals.CompletionTok != 55 {
		t.Fatalf("pt/ct = %d/%d, want 110/55", s.Totals.PromptTokens, s.Totals.CompletionTok)
	}
	if s.Totals.TotalTokens != 165 {
		t.Fatalf("tt = %d, want 165（无 total 时按 pt+ct 兜底）", s.Totals.TotalTokens)
	}
	if s.Totals.AvgLatencyMs != 200 {
		t.Fatalf("avg latency = %v, want 200", s.Totals.AvgLatencyMs)
	}
	if len(s.ByRealm) != 2 {
		t.Fatalf("by_realm = %d 项, want 2", len(s.ByRealm))
	}
	if s.ByAccount[0].Realm == "" {
		t.Fatal("by_account 行缺 realm 标注")
	}
	if len(s.Series) == 0 || s.Series[0].Models["glm-5.2"] != 165 {
		t.Fatalf("series[0].Models[glm-5.2] = %v, want 165", s.Series[0].Models["glm-5.2"])
	}
}

// Rollup 把超出 hourlyKeep 的小时桶折叠为日桶，且幂等：重复折叠不重复计数。
// 窗口口径：24h 窗口不含 100 天前的日桶；hours=0（全部历史）才含日点。
func TestRollupIdempotent(t *testing.T) {
	r := New("")
	old := time.Now().AddDate(0, 0, -100) // 100 天前，超出 90 天小时保留
	r.Add(old, "cn", "u", "m", "master", Delta{PromptTokens: 7, HasPromptTokens: true}, true)
	r.Add(old, "cn", "u", "m", "master", Delta{PromptTokens: 7, HasPromptTokens: true}, true)
	r.Add(time.Now(), "cn", "u", "m", "master", Delta{PromptTokens: 1, HasPromptTokens: true}, true)

	r.Rollup(time.Now())
	after := r.Snapshot(24, nil)
	if after.Totals.Requests != 1 || after.Totals.PromptTokens != 1 {
		t.Fatalf("24h 窗口 totals = %d/%d, want 1/1（窗口外日桶不进聚合）", after.Totals.Requests, after.Totals.PromptTokens)
	}
	if len(after.Series) != 1 || after.Series[0].Scope != "hour" {
		t.Fatalf("series = %+v, want 仅当前小时 1 个点", after.Series)
	}

	all := r.Snapshot(0, nil)
	if all.Totals.Requests != 3 || all.Totals.PromptTokens != 15 {
		t.Fatalf("全部历史 totals = %d/%d, want 3/15", all.Totals.Requests, all.Totals.PromptTokens)
	}
	if len(all.Series) != 2 || all.Series[0].Scope != "day" || all.Series[1].Scope != "hour" {
		t.Fatalf("series = %+v, want 日点在前 + 小时点在后", all.Series)
	}

	r.Rollup(time.Now())
	again := r.Snapshot(0, nil)
	if again.Totals.Requests != 3 || again.Totals.PromptTokens != 15 {
		t.Fatalf("二次折叠后 totals = %d/%d, want 3/15（幂等被破坏）", again.Totals.Requests, again.Totals.PromptTokens)
	}
}

// 落盘→新实例恢复，数据不丢；落盘结构带版本号。
func TestFlushLoadRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	r1 := New(path)
	r1.Add(time.Now(), "cn", "u1", "glm-5.2", "master", Delta{PromptTokens: 42, HasPromptTokens: true, TotalTokens: 42, HasTotal: true}, true)
	r1.Save()

	r2 := New(path)
	s := r2.Snapshot(24, nil)
	if s.Totals.Requests != 1 || s.Totals.TotalTokens != 42 {
		t.Fatalf("恢复后 totals = %d/%d, want 1/42", s.Totals.Requests, s.Totals.TotalTokens)
	}
	raw, _ := os.ReadFile(path)
	var f file
	if err := json.Unmarshal(raw, &f); err != nil || f.Version != 1 || len(f.Buckets) != 1 {
		t.Fatalf("落盘文件异常: err=%v buckets=%d", err, len(f.Buckets))
	}
}

// Snapshot 全口径窗口过滤：窗口外的数据不进**任何**聚合（卡片/表格/时序），
// 切窗口数字随之变化；hours=0 全部历史。Buckets 为窗口内命中的桶数。
func TestSnapshotWindowFilter(t *testing.T) {
	r := New("")
	now := time.Now()
	r.Add(now.Add(-48*time.Hour), "cn", "u", "m", "master", Delta{PromptTokens: 5, HasPromptTokens: true}, true) // 窗口(24h)外
	r.Add(now, "cn", "u", "m", "master", Delta{PromptTokens: 3, HasPromptTokens: true}, true)                    // 窗口内
	s := r.Snapshot(24, nil)
	if s.Totals.Requests != 1 || s.Totals.PromptTokens != 3 {
		t.Fatalf("24h 窗口 totals = %d/%d, want 1/3（48h 前的数据应被过滤）", s.Totals.Requests, s.Totals.PromptTokens)
	}
	if len(s.Series) != 1 || s.Series[0].Scope != "hour" || s.Series[0].PromptTokens != 3 {
		t.Fatalf("series = %+v, want 仅窗口内 1 个小时点", s.Series)
	}
	if s.Buckets != 1 {
		t.Fatalf("buckets = %d, want 1（窗口内命中桶数）", s.Buckets)
	}

	all := r.Snapshot(0, nil)
	if all.Totals.Requests != 2 || all.Totals.PromptTokens != 8 {
		t.Fatalf("全部历史 totals = %d/%d, want 2/8", all.Totals.Requests, all.Totals.PromptTokens)
	}
	// since 是全库数据起点，不受窗口影响。
	if all.Since == "" || s.Since != all.Since {
		t.Fatalf("since 应为全库起点且不随窗口变化: all=%q windowed=%q", all.Since, s.Since)
	}
}

// Stop 触发最终落盘（Start 后未到防抖间隔也要落）。
func TestLifecycleFlush(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	r := New(path)
	r.Start()
	r.Add(time.Now(), "cn", "u", "m", "master", Delta{PromptTokens: 9, HasPromptTokens: true}, true)
	r.Stop()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stop 后应有落盘文件: %v", err)
	}
}

// TestSeriesModelsBreakdown 验证 Snapshot 生成的时序点正确包含各模型 total_tokens 拆解，
// 且 JSON 序列化符合前端契约，旧聚合字段保持严格向后兼容。
func TestSeriesModelsBreakdown(t *testing.T) {
	r := New("")
	now := time.Now()

	// 1. 同一小时内分别记录 glm-5.2、kimi-k1.5、deepseek-chat
	// glm-5.2: 上游没有 total, pt=100, ct=50 -> 兜底 tt=150
	r.Add(now, "cn", "u1", "glm-5.2", "master", Delta{
		PromptTokens:     100,
		HasPromptTokens:  true,
		CompletionTokens: 50,
		HasCompletion:    true,
	}, true)

	// kimi-k1.5: 上游显式给 total=300 (pt=200, ct=100)
	r.Add(now, "cn", "u2", "kimi-k1.5", "master", Delta{
		PromptTokens:     200,
		HasPromptTokens:  true,
		CompletionTokens: 100,
		HasCompletion:    true,
		TotalTokens:      300,
		HasTotal:         true,
	}, true)

	// deepseek-chat: pt=60, ct=40 -> 兜底 tt=100
	r.Add(now, "cn", "u3", "deepseek-chat", "master", Delta{
		PromptTokens:     60,
		HasPromptTokens:  true,
		CompletionTokens: 40,
		HasCompletion:    true,
	}, true)

	// 失败重试/0 Token 请求：不计入 models
	r.Add(now, "cn", "u4", "fail-model", "master", Delta{}, false)

	// 2. 验证 Snapshot(24) 时序点
	s := r.Snapshot(24, nil)
	if len(s.Series) != 1 {
		t.Fatalf("series 点数 = %d, want 1", len(s.Series))
	}
	p := s.Series[0]
	if p.Scope != "hour" {
		t.Fatalf("series[0].Scope = %s, want hour", p.Scope)
	}
	if p.TotalTokens != 550 {
		t.Fatalf("series[0].TotalTokens = %d, want 550 (150+300+100)", p.TotalTokens)
	}
	if len(p.Models) != 3 {
		t.Fatalf("series[0].Models 种类数 = %d, want 3", len(p.Models))
	}
	if p.Models["glm-5.2"] != 150 {
		t.Errorf("Models[glm-5.2] = %d, want 150", p.Models["glm-5.2"])
	}
	if p.Models["kimi-k1.5"] != 300 {
		t.Errorf("Models[kimi-k1.5] = %d, want 300", p.Models["kimi-k1.5"])
	}
	if p.Models["deepseek-chat"] != 100 {
		t.Errorf("Models[deepseek-chat] = %d, want 100", p.Models["deepseek-chat"])
	}
	if _, exists := p.Models["fail-model"]; exists {
		t.Errorf("0 token 失败模型不应存在于 Models 中: %v", p.Models["fail-model"])
	}

	// 3. 验证 JSON 结构与契约规范
	raw, err := json.Marshal(s.Series)
	if err != nil {
		t.Fatalf("json.Marshal(s.Series) 失败: %v", err)
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("json.Unmarshal 失败: %v", err)
	}
	modelsMap, ok := out[0]["models"].(map[string]any)
	if !ok {
		t.Fatalf("序列化后未找到 models 映射: %+v", out[0])
	}
	if int64(modelsMap["kimi-k1.5"].(float64)) != 300 {
		t.Errorf("json models[kimi-k1.5] = %v, want 300", modelsMap["kimi-k1.5"])
	}

	// 4. 验证日点聚合中的 models breakdown
	oldDay := now.AddDate(0, 0, -100)
	r.Add(oldDay, "cn", "u1", "claude-3.5", "master", Delta{
		PromptTokens:     500,
		HasPromptTokens:  true,
		CompletionTokens: 200,
		HasCompletion:    true,
	}, true)
	r.Rollup(now)

	all := r.Snapshot(0, nil)
	var dayPoint *Point
	for i := range all.Series {
		if all.Series[i].Scope == "day" {
			dayPoint = &all.Series[i]
			break
		}
	}
	if dayPoint == nil {
		t.Fatal("全部历史中未找到折叠后的日点")
	}
	if dayPoint.Models["claude-3.5"] != 700 {
		t.Errorf("dayPoint.Models[claude-3.5] = %d, want 700", dayPoint.Models["claude-3.5"])
	}
}

// TestCreditTrackingAndDefense 验证官方积分打通：真实扣费累加、NaN/Inf/负数防御、4位小数修约及落盘持久化。
func TestCreditTrackingAndDefense(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage_credit.json")
	r := New(path)
	now := time.Now()

	// 1. 合法扣费累加与 CreditRequests 计数
	r.Add(now, "cn", "u1", "glm-5.2", "master", Delta{
		PromptTokens:     100,
		HasPromptTokens:  true,
		CompletionTokens: 50,
		HasCompletion:    true,
		Credit:           0.0525,
		HasCredit:        true,
	}, true)

	r.Add(now, "cn", "u1", "glm-5.2", "master", Delta{
		PromptTokens:     200,
		HasPromptTokens:  true,
		CompletionTokens: 100,
		HasCompletion:    true,
		Credit:           0.1042,
		HasCredit:        true,
	}, true)

	// 2. 只有 token 没有 credit 的请求：不增加 credit，不增加 CreditRequests
	r.Add(now, "cn", "u1", "glm-5.2", "master", Delta{
		PromptTokens:     50,
		HasPromptTokens:  true,
		CompletionTokens: 25,
		HasCompletion:    true,
		HasCredit:        false,
	}, true)

	// 3. 致命漏洞防御：NaN, +Inf, -Inf, 负数, 超大值（>1e9）必须被拦截
	r.Add(now, "cn", "u1", "glm-5.2", "master", Delta{
		Credit:    math.NaN(),
		HasCredit: true,
	}, true)
	r.Add(now, "cn", "u1", "glm-5.2", "master", Delta{
		Credit:    math.Inf(1),
		HasCredit: true,
	}, true)
	r.Add(now, "cn", "u1", "glm-5.2", "master", Delta{
		Credit:    math.Inf(-1),
		HasCredit: true,
	}, true)
	r.Add(now, "cn", "u1", "glm-5.2", "master", Delta{
		Credit:    -0.5,
		HasCredit: true,
	}, true)
	r.Add(now, "cn", "u1", "glm-5.2", "master", Delta{
		Credit:    1e11,
		HasCredit: true,
	}, true)

	s := r.Snapshot(24, nil)
	// 期望 0.0525 + 0.1042 = 0.1567
	if s.Totals.Credits != 0.1567 {
		t.Fatalf("Totals.Credits = %v, want 0.1567", s.Totals.Credits)
	}
	if s.Totals.CreditRequests != 2 {
		t.Fatalf("Totals.CreditRequests = %d, want 2", s.Totals.CreditRequests)
	}

	// 4. 落盘并恢复，检验 JSON 序列化无 panic 且数据不丢
	r.Save()

	r2 := New(path)
	s2 := r2.Snapshot(24, nil)
	if s2.Totals.Credits != 0.1567 {
		t.Fatalf("恢复后 Totals.Credits = %v, want 0.1567", s2.Totals.Credits)
	}
	if s2.Totals.CreditRequests != 2 {
		t.Fatalf("恢复后 Totals.CreditRequests = %d, want 2", s2.Totals.CreditRequests)
	}

	// 5. 日桶折叠测试：超出 hourlyKeep 的小时桶折叠后 Credit 与 CreditReq 完整保留
	old := now.AddDate(0, 0, -100)
	r.Add(old, "cn", "u1", "glm-5.2", "master", Delta{
		Credit:    0.2000,
		HasCredit: true,
	}, true)
	r.Rollup(now)

	all := r.Snapshot(0, nil)
	// 0.1567 + 0.2000 = 0.3567
	if all.Totals.Credits != 0.3567 {
		t.Fatalf("全部历史 Totals.Credits = %v, want 0.3567", all.Totals.Credits)
	}
	if all.Totals.CreditRequests != 3 {
		t.Fatalf("全部历史 Totals.CreditRequests = %d, want 3", all.Totals.CreditRequests)
	}
}

// TestNormalizeDeepseekFlashModel 验证 deepseek-v4-flash 与 deepseek-v4.1-flash 在快照中自动归类合并为 deepseek-v4.1-flash。
func TestNormalizeDeepseekFlashModel(t *testing.T) {
	r := New("")
	now := time.Now()

	// 记录 2 次 deepseek-v4-flash
	r.Add(now, "cn", "u1", "deepseek-v4-flash", "master", Delta{
		PromptTokens:     100,
		HasPromptTokens:  true,
		CompletionTokens: 50,
		HasCompletion:    true,
		TotalTokens:      150,
		HasTotal:         true,
	}, true)
	r.Add(now, "cn", "u2", "deepseek-v4-flash", "master", Delta{
		PromptTokens:     200,
		HasPromptTokens:  true,
		CompletionTokens: 100,
		HasCompletion:    true,
		TotalTokens:      300,
		HasTotal:         true,
	}, true)

	// 记录 1 次 deepseek-v4.1-flash
	r.Add(now, "cn", "u1", "deepseek-v4.1-flash", "master", Delta{
		PromptTokens:     50,
		HasPromptTokens:  true,
		CompletionTokens: 25,
		HasCompletion:    true,
		TotalTokens:      75,
		HasTotal:         true,
	}, true)

	snap := r.Snapshot(24, nil)

	// 1. ByModel 应该只有 1 个模型项，且名称为 deepseek-v4.1-flash
	if len(snap.ByModel) != 1 {
		t.Fatalf("expected 1 model entry, got %d: %+v", len(snap.ByModel), snap.ByModel)
	}
	m := snap.ByModel[0]
	if m.Key != "deepseek-v4.1-flash" {
		t.Errorf("expected model key 'deepseek-v4.1-flash', got '%s'", m.Key)
	}
	if m.Requests != 3 {
		t.Errorf("expected 3 requests merged, got %d", m.Requests)
	}
	if m.TotalTokens != 525 {
		t.Errorf("expected 525 total tokens, got %d", m.TotalTokens)
	}

	// 2. Series 时序分解中也应合并为 deepseek-v4.1-flash
	if len(snap.Series) != 1 {
		t.Fatalf("expected 1 series point, got %d", len(snap.Series))
	}
	p := snap.Series[0]
	if _, hasOld := p.Models["deepseek-v4-flash"]; hasOld {
		t.Errorf("series should not contain deepseek-v4-flash, found: %v", p.Models)
	}
	if p.Models["deepseek-v4.1-flash"] != 525 {
		t.Errorf("series deepseek-v4.1-flash tokens want 525, got %d", p.Models["deepseek-v4.1-flash"])
	}
}

