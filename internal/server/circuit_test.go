package server

import (
	"testing"
	"time"
)

// TestCircuitBreakerHealthyByDefault 新建的模型默认健康。
func TestCircuitBreakerHealthyByDefault(t *testing.T) {
	b := NewModelCircuitBreaker()
	isDeg, fb := b.Check("glm-5.3")
	if isDeg || fb != "" {
		t.Errorf("new model should be healthy, got degraded=%v fallback=%q", isDeg, fb)
	}
}

// TestCircuitBreakerDegradeAndRecover 降级后在恢复时间到期前保持降级，到期后放行探测。
func TestCircuitBreakerDegradeAndRecover(t *testing.T) {
	b := NewModelCircuitBreaker()
	recovery := time.Now().Add(2 * time.Second)
	b.RecordDegraded("glm-5.3", "deepseek-v4-pro", recovery, "all 429")

	// 降级期内应返回降级
	isDeg, fb := b.Check("glm-5.3")
	if !isDeg || fb != "deepseek-v4-pro" {
		t.Errorf("should be degraded, got degraded=%v fallback=%q", isDeg, fb)
	}

	// 快照应显示 degraded
	snap := b.Snapshot()
	if len(snap) != 1 || snap[0].Status != "degraded" {
		t.Errorf("snapshot should show degraded, got %+v", snap)
	}
	if snap[0].RemainingSec <= 0 {
		t.Errorf("remaining seconds should be positive, got %d", snap[0].RemainingSec)
	}
}

// TestCircuitBreakerRecoveryExpiry 恢复时间到期后 Check 应放行（不再降级）。
// RecordDegraded 会将过去时间兜底为 now+60s，所以通过直接设置内部状态测试到期逻辑。
func TestCircuitBreakerRecoveryExpiry(t *testing.T) {
	b := NewModelCircuitBreaker()
	// 先正常降级
	b.RecordDegraded("glm-5.3", "deepseek-v4-pro", time.Now().Add(time.Hour), "all 429")

	// 直接修改内部状态模拟到期（测试内部访问同包）
	b.mu.Lock()
	b.models["glm-5.3"].degradedUntil = time.Now().Add(-1 * time.Second)
	b.mu.Unlock()

	isDeg, fb := b.Check("glm-5.3")
	if isDeg || fb != "" {
		t.Errorf("recovery expired, should not be degraded, got degraded=%v fallback=%q", isDeg, fb)
	}

	// 快照应显示 probing
	snap := b.Snapshot()
	if len(snap) != 1 || snap[0].Status != "probing" {
		t.Errorf("expired degradation should show probing, got %+v", snap)
	}
}

// TestCircuitBreakerRecordSuccess 成功调用应恢复为健康。
func TestCircuitBreakerRecordSuccess(t *testing.T) {
	b := NewModelCircuitBreaker()
	b.RecordDegraded("glm-5.3", "deepseek-v4-pro", time.Now().Add(time.Hour), "all 429")

	// 成功后恢复
	b.RecordSuccess("glm-5.3")

	isDeg, fb := b.Check("glm-5.3")
	if isDeg || fb != "" {
		t.Errorf("after success, should be healthy, got degraded=%v fallback=%q", isDeg, fb)
	}
}

// TestCircuitBreakerRevive 手动恢复。
func TestCircuitBreakerRevive(t *testing.T) {
	b := NewModelCircuitBreaker()
	b.RecordDegraded("glm-5.3", "deepseek-v4-pro", time.Now().Add(time.Hour), "all 429")

	b.Revive("glm-5.3")

	isDeg, fb := b.Check("glm-5.3")
	if isDeg || fb != "" {
		t.Errorf("after revive, should be healthy, got degraded=%v fallback=%q", isDeg, fb)
	}
}

// TestCircuitBreakerDefaultRecovery 零值恢复时间应兜底 60 秒。
func TestCircuitBreakerDefaultRecovery(t *testing.T) {
	b := NewModelCircuitBreaker()
	b.RecordDegraded("glm-5.3", "deepseek-v4-pro", time.Time{}, "all 429")

	// 应该降级（兜底 60 秒）
	isDeg, _ := b.Check("glm-5.3")
	if !isDeg {
		t.Error("zero recovery time should default to 60s degradation")
	}

	snap := b.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 snapshot entry, got %d", len(snap))
	}
	// 兜底 60 秒，remaining 应该在 55-65 之间
	if snap[0].RemainingSec < 50 || snap[0].RemainingSec > 65 {
		t.Errorf("default recovery remaining should be ~60s, got %d", snap[0].RemainingSec)
	}
}

// TestCircuitBreakerMultiModel 多模型独立降级。
func TestCircuitBreakerMultiModel(t *testing.T) {
	b := NewModelCircuitBreaker()
	b.RecordDegraded("glm-5.3", "deepseek-v4-pro", time.Now().Add(time.Hour), "429")
	b.RecordDegraded("kimi-k3-1", "glm-5.2", time.Now().Add(30*time.Minute), "429")

	// glm-5.3 降级
	isDeg, fb := b.Check("glm-5.3")
	if !isDeg || fb != "deepseek-v4-pro" {
		t.Errorf("glm-5.3 should be degraded to deepseek-v4-pro")
	}

	// kimi-k3-1 降级
	isDeg, fb = b.Check("kimi-k3-1")
	if !isDeg || fb != "glm-5.2" {
		t.Errorf("kimi-k3-1 should be degraded to glm-5.2")
	}

	// 恢复 glm-5.3 不影响 kimi-k3-1
	b.RecordSuccess("glm-5.3")
	isDeg, _ = b.Check("glm-5.3")
	if isDeg {
		t.Error("glm-5.3 should be healthy after success")
	}
	isDeg, _ = b.Check("kimi-k3-1")
	if !isDeg {
		t.Error("kimi-k3-1 should still be degraded")
	}

	snap := b.Snapshot()
	if len(snap) != 2 {
		t.Errorf("expected 2 snapshot entries, got %d", len(snap))
	}
}

// TestCircuitBreakerSuccessOnHealthy 健康模型调用 RecordSuccess 无副作用。
func TestCircuitBreakerSuccessOnHealthy(t *testing.T) {
	b := NewModelCircuitBreaker()
	// 对从未降级的模型调用 RecordSuccess 不应 panic 或产生副作用
	b.RecordSuccess("nonexistent-model")
	snap := b.Snapshot()
	if len(snap) != 0 {
		t.Errorf("snapshot should be empty, got %+v", snap)
	}
}
