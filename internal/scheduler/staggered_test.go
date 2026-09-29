package scheduler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/pool"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

func TestRunBalanceRefreshNowCASLock(t *testing.T) {
	oldMin, oldMax := balanceAccountDelayMin, balanceAccountDelayMax
	balanceAccountDelayMin = 0
	balanceAccountDelayMax = 0
	t.Cleanup(func() {
		balanceAccountDelayMin, balanceAccountDelayMax = oldMin, oldMax
	})

	var queryCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queryCalls.Add(1)
		time.Sleep(50 * time.Millisecond) // 模拟查询耗时
		w.Write([]byte(`{"code":0,"data":{"Response":{"Data":{"Accounts":[{"CycleCapacitySize":1000,"CycleCapacityRemain":500,"CycleCapacityUsed":0}]}}}}`))
	}))
	defer srv.Close()

	p := pool.New("")
	p.Add(&auth.Auth{UID: "u1", AccessToken: "at", RefreshToken: "rt", ExpiresAt: 9999999999})
	up := &upstream.Client{HTTP: srv.Client(), ChatBaseCN: srv.URL, BillingBaseCN: srv.URL}
	s := New(Config{Pool: p, Upstream: up})

	// 并发触发两个 RunBalanceRefreshNow
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		s.RunBalanceRefreshNow()
	}()
	go func() {
		defer wg.Done()
		s.RunBalanceRefreshNow()
	}()
	wg.Wait()

	// 由于 CAS 锁防护，重叠并发请求中其中一个必被跳过，查询只发生 1 次
	if calls := queryCalls.Load(); calls != 1 {
		t.Fatalf("CAS lock 应防抖拒绝重叠并发，实际发生查询次数: %d", calls)
	}
}

func TestStaggeredWorkerLivenessGuard(t *testing.T) {
	oldColdMin, oldColdMax := balanceColdStartMin, balanceColdStartMax
	balanceColdStartMin = 0
	balanceColdStartMax = 0
	t.Cleanup(func() {
		balanceColdStartMin, balanceColdStartMax = oldColdMin, oldColdMax
	})

	var queryCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queryCalls.Add(1)
		w.Write([]byte(`{"code":0,"data":{"Response":{"Data":{"Accounts":[{"CycleCapacitySize":1000,"CycleCapacityRemain":888,"CycleCapacityUsed":0}]}}}}`))
	}))
	defer srv.Close()

	p := pool.New("")
	p.Add(&auth.Auth{UID: "u_guard", AccessToken: "at", RefreshToken: "rt", ExpiresAt: 9999999999})
	up := &upstream.Client{HTTP: srv.Client(), ChatBaseCN: srv.URL, BillingBaseCN: srv.URL}
	s := New(Config{Pool: p, Upstream: up})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.StartBalanceRefresh(ctx, 5*time.Minute)

	// 等待 Worker 启动并初次刷新
	deadline := time.Now().Add(2 * time.Second)
	for queryCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if queryCalls.Load() == 0 {
		t.Fatal("Worker 未完成首次刷新")
	}

	// 验证 Worker 在活跃列表内
	s.balanceWorkersMu.Lock()
	_, active := s.balanceWorkers["u_guard"]
	s.balanceWorkersMu.Unlock()
	if !active {
		t.Fatal("Worker 应当处于活跃列表")
	}

	// 模拟账号被出池或禁用
	p.Disable("u_guard", "test disabled")

	// 触发唤醒通道，让 Worker 进行存活性检测
	s.notifyAllWorkers()

	// 等待 Worker clean exit
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.balanceWorkersMu.Lock()
		_, stillActive := s.balanceWorkers["u_guard"]
		s.balanceWorkersMu.Unlock()
		if !stillActive {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	s.balanceWorkersMu.Lock()
	_, stillActive := s.balanceWorkers["u_guard"]
	s.balanceWorkersMu.Unlock()
	if stillActive {
		t.Fatal("存活性守卫：已禁用的账号 Worker 应当 clean exit 退出协程")
	}
}
