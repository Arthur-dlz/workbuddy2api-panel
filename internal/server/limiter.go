package server

import (
	"sync"
	"time"
)

// ModelLimiter 管理单模型并发数 (MaxInFlight) 与 RPM 限流。
type ModelLimiter struct {
	mu        sync.Mutex
	inFlight  map[string]int
	rpmWindow map[string][]time.Time
}

func newModelLimiter() *ModelLimiter {
	return &ModelLimiter{
		inFlight:  make(map[string]int),
		rpmWindow: make(map[string][]time.Time),
	}
}

// Acquire 尝试占用单模型并发或 RPM 配额。若超限返回错误代码，成功返回 release 闭包。
func (l *ModelLimiter) Acquire(model string, maxInFlight, rpm int) (release func(), limitErr string) {
	if maxInFlight <= 0 && rpm <= 0 {
		return func() {}, ""
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	// 1. 检查单模型并发上限 (MaxInFlight)
	if maxInFlight > 0 {
		if l.inFlight[model] >= maxInFlight {
			return nil, "concurrent_limit"
		}
	}

	// 2. 检查单模型 RPM 限流
	if rpm > 0 {
		windowStart := now.Add(-time.Minute)
		timestamps := l.rpmWindow[model]
		validIdx := 0
		for validIdx < len(timestamps) && timestamps[validIdx].Before(windowStart) {
			validIdx++
		}
		if validIdx > 0 {
			timestamps = timestamps[validIdx:]
		}
		if len(timestamps) >= rpm {
			l.rpmWindow[model] = timestamps
			return nil, "rpm_limit"
		}
		l.rpmWindow[model] = append(timestamps, now)
	}

	l.inFlight[model]++

	var once sync.Once
	release = func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			if l.inFlight[model] > 0 {
				l.inFlight[model]--
			}
		})
	}

	return release, ""
}

// InFlightCount 返回模型当前在途请求数
func (l *ModelLimiter) InFlightCount(model string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.inFlight[model]
}
