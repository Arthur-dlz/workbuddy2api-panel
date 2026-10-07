package scheduler

import (
	"context"
	"log"
	"math/rand/v2"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/logfmt"
)

var (
	// balanceAccountDelayMin / balanceAccountDelayMax 全量手动刷新时账号间的拟人间隔（800ms~1200ms）。测试可置 0。
	balanceAccountDelayMin = 800 * time.Millisecond
	balanceAccountDelayMax = 1200 * time.Millisecond

	// balanceColdStartMin / balanceColdStartMax 冷启动平滑打散范围（[5s, 300s]）。测试可置 0。
	balanceColdStartMin = 5 * time.Second
	balanceColdStartMax = 300 * time.Second
)

func balanceHumanDelay() time.Duration {
	if balanceAccountDelayMin <= 0 || balanceAccountDelayMax <= balanceAccountDelayMin {
		return 0
	}
	span := int64(balanceAccountDelayMax - balanceAccountDelayMin)
	r := rand.Int64N(span + 1)
	return balanceAccountDelayMin + time.Duration(r)
}

// RunBalanceRefreshNow 手动刷新全量账号余额（SingleFlight / CAS Lock 并发保护）。
// 若正在执行则拒绝重复并发；账号间强制注入 800ms~1200ms 拟人间隔。
func (s *Scheduler) RunBalanceRefreshNow() {
	s.RunBalanceRefreshContext(context.Background())
}

// RunBalanceRefreshContext refreshes balances while honoring ctx cancellation.
func (s *Scheduler) RunBalanceRefreshContext(ctx context.Context) {
	if !s.balanceRefreshing.CompareAndSwap(false, true) {
		log.Printf("[balance-pi] 手动刷新正在执行中，跳过重复并发")
		return
	}
	defer s.balanceRefreshing.Store(false)

	expiringSoon := s.ExpiringSoonWindow()
	first := true
	for _, st := range s.cfg.Pool.List() {
		if ctx.Err() != nil {
			return
		}
		if st.Disabled {
			continue
		}
		a := s.cfg.Pool.AuthByUID(st.UID)
		if a == nil {
			continue
		}
		if !first {
			d := balanceHumanDelay()
			if d > 0 {
				if !sleepCtx(ctx, d) {
					return
				}
			}
		}
		first = false

		remain, total, expiring, earliestAt, earliestRemaining, err := s.cfg.Upstream.UserResourceDetailedWithExpiryContext(ctx, a, expiringSoon)
		nick := a.NicknameValue()
		if err != nil {
			log.Printf("[balance-pi] %s: 手动刷新失败: %v", logfmt.Label(st.UID, nick), err)
			continue
		}
		s.cfg.Pool.ReenableIfCredits(st.UID, remain, total)
		s.cfg.Pool.SetCreditsDetailed(st.UID, remain, total, expiring, earliestAt, earliestRemaining)
		log.Printf("[balance-pi] %s: 手动刷新成功 remain=%d total=%d", logfmt.Label(st.UID, nick), remain, total)
	}
}

// StartBalanceRefresh 后台基于 π 混沌序列的单账号交错轮询调度。
// 各账号独立轻量 Worker 循环，冷启动平滑打散在 [5s, 300s] 内，存活性守卫在账号被删除/禁用时 clean exit。
// 运行期可用 SetBalanceInterval 热改间隔（下一轮生效，<=0 暂停）。
func (s *Scheduler) StartBalanceRefresh(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		s.balanceInterval.Store(0)
	} else {
		s.balanceInterval.Store(int64(interval))
	}

	s.workers.Add(1)
	go func() { defer s.workers.Done(); s.runBalanceRefreshSupervisor(ctx) }()
}

// SetBalanceInterval 热改余额刷新间隔；<=0 表示暂停循环（面板关闭该开关时）。
func (s *Scheduler) SetBalanceInterval(d time.Duration) {
	if d < 0 {
		d = 0
	}
	s.balanceInterval.Store(int64(d))
	poke(s.rearmBalance)
}

func (s *Scheduler) runBalanceRefreshSupervisor(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	// 启动时拉起初始各账号 Worker
	s.syncBalanceWorkers(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.rearmBalance:
			// 配置热改：唤醒现有活跃 Worker 并同步新号
			s.notifyAllWorkers()
			s.syncBalanceWorkers(ctx)
		case <-ticker.C:
			// 周期检查是否有新账号加入池中
			s.syncBalanceWorkers(ctx)
		}
	}
}

func (s *Scheduler) syncBalanceWorkers(ctx context.Context) {
	if s.cfg.Pool == nil {
		return
	}
	cur := time.Duration(s.balanceInterval.Load())
	if cur <= 0 {
		return // 暂停中暂不拉起新 Worker
	}

	for _, st := range s.cfg.Pool.List() {
		if st.Disabled {
			continue
		}
		s.ensureBalanceWorker(ctx, st.UID)
	}
}

func (s *Scheduler) ensureBalanceWorker(ctx context.Context, uid string) {
	s.balanceWorkersMu.Lock()
	defer s.balanceWorkersMu.Unlock()

	if _, exists := s.balanceWorkers[uid]; exists {
		return
	}

	wakeCh := make(chan struct{}, 1)
	s.balanceWorkers[uid] = wakeCh

	s.workers.Add(1)
	go func() { defer s.workers.Done(); s.runSingleAccountBalanceWorker(ctx, uid, wakeCh) }()
}

func (s *Scheduler) removeBalanceWorker(uid string) {
	s.balanceWorkersMu.Lock()
	defer s.balanceWorkersMu.Unlock()
	delete(s.balanceWorkers, uid)
}

func (s *Scheduler) notifyAllWorkers() {
	s.balanceWorkersMu.Lock()
	defer s.balanceWorkersMu.Unlock()
	for _, wakeCh := range s.balanceWorkers {
		select {
		case wakeCh <- struct{}{}:
		default:
		}
	}
}

func (s *Scheduler) runSingleAccountBalanceWorker(ctx context.Context, uid string, wakeCh chan struct{}) {
	defer s.removeBalanceWorker(uid)

	pi := NewPiSchedule(uid)

	// 冷启动打散：首次执行时间打散在 [5s, 300s] 内，杜绝启动瞬发并发
	coldDelay := pi.InitialDelay()
	if balanceColdStartMax > balanceColdStartMin && balanceColdStartMin > 0 {
		span := int64(balanceColdStartMax - balanceColdStartMin)
		coldDelay = balanceColdStartMin + time.Duration(int64(coldDelay)%span)
	} else if balanceColdStartMin == 0 {
		coldDelay = 0
	}

	a := s.cfg.Pool.AuthByUID(uid)
	nick := ""
	if a != nil {
		nick = a.NicknameValue()
	}
	log.Printf("[balance-pi] %s: 冷启动调度，首次执行在 %s 后", logfmt.Label(uid, nick), coldDelay)

	if coldDelay > 0 {
		timer := time.NewTimer(coldDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-wakeCh:
			timer.Stop()
		case <-timer.C:
		}
	}

	for {
		// 1. 配置热改检查：interval <= 0 时 Worker 暂停等待唤醒
		curInterval := time.Duration(s.balanceInterval.Load())
		if curInterval <= 0 {
			select {
			case <-ctx.Done():
				return
			case <-wakeCh:
				continue
			}
		}

		// 2. 存活性守卫（防幽灵复活与泄漏）：每次 Worker 唤醒时，第一步校验账号是否仍存在于池中且未被删除/禁用，已删除/禁用则 clean exit 退出协程
		st, ok := s.cfg.Pool.Status(uid)
		if !ok || st.Disabled {
			log.Printf("[balance-pi] %s: 账号已出池或被禁用，Worker 退出", uid)
			return
		}
		acct := s.cfg.Pool.AuthByUID(uid)
		if acct == nil {
			log.Printf("[balance-pi] %s: 凭证已删除，Worker 退出", uid)
			return
		}

		// 3. 执行查询与更新
		expiringSoon := s.ExpiringSoonWindow()
		if ctx.Err() != nil {
			return
		}
		remain, total, expiring, earliestAt, earliestRemaining, err := s.cfg.Upstream.UserResourceDetailedWithExpiryContext(ctx, acct, expiringSoon)

		// 4. 计算下一次执行间隔
		nextDelay := pi.NextDelay()
		if curInterval != 5*time.Minute && curInterval > 0 {
			nextDelay = pi.NextDelayForInterval(curInterval)
		}

		if err != nil {
			log.Printf("[balance-pi] %s: 刷新余额失败: %v，下次执行时长 %s", logfmt.Label(uid, acct.NicknameValue()), err, nextDelay)
		} else {
			s.cfg.Pool.ReenableIfCredits(uid, remain, total)
			s.cfg.Pool.SetCreditsDetailed(uid, remain, total, expiring, earliestAt, earliestRemaining)
			log.Printf("[balance-pi] %s: 刷新余额成功 remain=%d total=%d，下次执行时长 %s", logfmt.Label(uid, acct.NicknameValue()), remain, total, nextDelay)
		}

		// 5. 等待下次执行
		timer := time.NewTimer(nextDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-wakeCh:
			timer.Stop()
			continue
		case <-timer.C:
		}
	}
}
