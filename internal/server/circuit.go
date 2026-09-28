package server

import (
	"log"
	"sync"
	"time"
)

// ModelBreakerInfo 用于对外暴露模型熔断/降级状态
type ModelBreakerInfo struct {
	Model          string    `json:"model"`
	Status         string    `json:"status"` // "healthy", "degraded", "probing"
	DegradedUntil  time.Time `json:"degraded_until,omitempty"`
	RemainingSec   int       `json:"remaining_sec"`
	Reason         string    `json:"reason,omitempty"`
	ActiveFallback string    `json:"active_fallback,omitempty"`
	LastDegradedAt time.Time `json:"last_degraded_at,omitempty"`
}

type modelBreakerItem struct {
	status         string    // "healthy", "degraded"
	degradedUntil  time.Time // 恢复时刻
	reason         string    // 降级原因
	activeFallback string    // 激活的备用降级模型
	lastDegradedAt time.Time
}

// ModelCircuitBreaker 负责管理全池模型级别的 429 智能熔断降级与自动恢复
type ModelCircuitBreaker struct {
	mu     sync.RWMutex
	models map[string]*modelBreakerItem
}

// NewModelCircuitBreaker 创建模型熔断降级管理器
func NewModelCircuitBreaker() *ModelCircuitBreaker {
	return &ModelCircuitBreaker{
		models: make(map[string]*modelBreakerItem),
	}
}

// Check 检查该模型是否处于降级状态：
// - 若处于降级中且未到恢复时间，返回 true 与当前激活的 fallback 模型；
// - 若已到恢复时间，或者原本健康，返回 false, ""，允许请求尝试主模型（触发自动升级恢复探测）。
func (b *ModelCircuitBreaker) Check(model string) (isDegraded bool, fallback string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	st, ok := b.models[model]
	if !ok || st.status != "degraded" {
		return false, ""
	}
	if time.Now().Before(st.degradedUntil) {
		return true, st.activeFallback
	}
	// 到期了，进入恢复探测阶段（放行主模型）
	return false, ""
}

// RecordSuccess 当主模型请求成功 (HTTP 200) 时调用：
// 若此前处于降级或探测期，立即将其恢复为正常状态！
func (b *ModelCircuitBreaker) RecordSuccess(model string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	st, ok := b.models[model]
	if ok && st.status == "degraded" {
		st.status = "healthy"
		st.activeFallback = ""
		st.reason = ""
		log.Printf("[circuit]  模型 %s 恢复时间已到，主模型调用成功，已自动升级恢复为主模型！", model)
	}
}

// RecordDegraded 当全池账号均返回 429 或额度耗尽时调用：
// 记录该模型的恢复时间并切换至 fallback 降级模型。
func (b *ModelCircuitBreaker) RecordDegraded(model, fallback string, recoveryTime time.Time, reason string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	// 若未指定恢复时间或时间已过去，兜底设定 60 秒恢复窗口
	if recoveryTime.IsZero() || !recoveryTime.After(now) {
		recoveryTime = now.Add(60 * time.Second)
	}
	st := b.models[model]
	if st == nil {
		st = &modelBreakerItem{}
		b.models[model] = st
	}
	st.status = "degraded"
	st.degradedUntil = recoveryTime
	st.activeFallback = fallback
	st.reason = reason
	st.lastDegradedAt = now

	dur := recoveryTime.Sub(now).Round(time.Second)
	log.Printf("[circuit] ⚡ 模型 %s 全池触发 429/无额度，自动降级至备选模型 %s，预计 %s 恢复 (冷却 %s 后自动升级回升)",
		model, fallback, recoveryTime.Format("15:04:05"), dur)
}

// Revive 手动重置恢复模型为健康状态
func (b *ModelCircuitBreaker) Revive(model string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if st, ok := b.models[model]; ok {
		st.status = "healthy"
		st.activeFallback = ""
		st.reason = ""
		log.Printf("[circuit]  模型 %s 已被手动恢复为主模型正常状态", model)
	}
}

// Snapshot 获取全部模型的当前熔断降级状态快照
func (b *ModelCircuitBreaker) Snapshot() []ModelBreakerInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()
	now := time.Now()
	out := make([]ModelBreakerInfo, 0, len(b.models))
	for m, st := range b.models {
		info := ModelBreakerInfo{
			Model:          m,
			Status:         st.status,
			DegradedUntil:  st.degradedUntil,
			Reason:         st.reason,
			ActiveFallback: st.activeFallback,
			LastDegradedAt: st.lastDegradedAt,
		}
		if st.status == "degraded" {
			if st.degradedUntil.After(now) {
				info.RemainingSec = int(st.degradedUntil.Sub(now).Seconds())
			} else {
				info.Status = "probing" // 冷却已过，等待探测请求
			}
		}
		out = append(out, info)
	}
	return out
}
