// streak.go 连登管家：签到排程后自动检查连登兑换档位 → 可兑换即兑换 → 按抽奖次数抽奖。
//
// 背景（2026-09-12）：成长中心连登档位（7d/14d/28d）按连续登录天数解锁，兑换发
// credit/energy/补签卡/抽奖次数；抽奖次数只能从兑换获得。兑换按钮在 UI 上恒可点，
// 但未解锁时服务端 403「连续登录天数不足」——所以放在每日签到后跑一遍（幂等），
// 到天数那天自动完成「兑换 → 抽奖」闭环，无需人工盯。
package scheduler

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/logfmt"
)

// RunStreakBonusNow 对所有可用账号执行连登兑换 + 抽奖（幂等：locked/无次数自动跳过）。
// 由签到排程（RunCheckinNow）末尾调用；也可面板手动触发。
func (s *Scheduler) RunStreakBonusNow() {
	s.RunStreakBonusContext(context.Background())
}

// RunStreakBonusContext executes the idempotent streak flow within ctx.
func (s *Scheduler) RunStreakBonusContext(ctx context.Context) {
	for _, st := range s.cfg.Pool.List() {
		if ctx.Err() != nil {
			return
		}
		if st.Disabled {
			continue
		}
		a := s.cfg.Pool.AuthByUID(st.UID)
		if a == nil || a.AccessTokenValue() == "" {
			continue
		}
		if a.IsGlobal() {
			continue // D4 门控：global 无 CN 任务体系，不发起任何上游调用
		}
		s.streakBonusAccountContext(ctx, a)
	}
}

// streakBonusAccount 单账号：补签保连登 → 礼包/补偿 → 兑换所有已解锁档位 → 抽完所有 chances。
func (s *Scheduler) streakBonusAccount(a *auth.Auth) {
	s.streakBonusAccountContext(context.Background(), a)
}

func (s *Scheduler) streakBonusAccountContext(ctx context.Context, a *auth.Auth) {
	// 0. 补签保连登：昨日漏签且有补签卡则补上（连续天数一断就要重攒 7 天）。
	s.makeupYesterdayContext(ctx, a)
	// 0.5 礼包/补偿（每号一次，无则业务错误静默跳过）。
	if ctx.Err() != nil {
		return
	}
	if credit, err := s.cfg.Upstream.ClaimGiftContext(ctx, a); err == nil {
		log.Printf("streak-bonus %s: 🎊 新手礼包 +%dc", logfmt.Label(a.UID, a.Nickname), credit)
	}
	if ctx.Err() != nil {
		return
	}
	if credit, err := s.cfg.Upstream.ClaimCompensationContext(ctx, a); err == nil {
		log.Printf("streak-bonus %s: 🎊 补偿领取 +%dc", logfmt.Label(a.UID, a.Nickname), credit)
	}

	full, err := s.cfg.Upstream.GrowthStreakFullContext(ctx, a)
	if err != nil {
		log.Printf("streak-bonus %s: %v", logfmt.Label(a.UID, a.Nickname), err)
		return
	}
	statuses := map[string]string{
		"7d":  full.RedemptionStatus.Tier7dStatus,
		"14d": full.RedemptionStatus.Tier14dStatus,
		"28d": full.RedemptionStatus.Tier28dStatus,
	}
	for _, tier := range full.RedemptionStatus.Tiers {
		if ctx.Err() != nil {
			return
		}
		status := statuses[tier.Tier]
		if status == "locked" || status == "claimed" {
			continue
		}
		if err := s.cfg.Upstream.GrowthRedeemTierContext(ctx, a, tier.Tier); err != nil {
			// 未解锁（403）属预期，静默；其余记日志。
			log.Printf("streak-bonus %s: redeem %s: %v", logfmt.Label(a.UID, a.Nickname), tier.Tier, err)
			continue
		}
		log.Printf("streak-bonus %s: ★ 兑换 %s 档（+%dc +%de 卡×%d 抽奖×%d）",
			a.UID, tier.Tier, tier.Credit, tier.Energy, tier.Cards, tier.Chances)
	}
	// 抽奖：按当前 chances 全抽完（兑换刚发的次数已在服务端累加）。
	chances, err := s.cfg.Upstream.LotteryChancesContext(ctx, a)
	if err != nil {
		log.Printf("streak-bonus %s: lottery summary: %v", logfmt.Label(a.UID, a.Nickname), err)
		return
	}
	for i := 0; i < chances; i++ {
		if ctx.Err() != nil {
			return
		}
		raw, err := s.cfg.Upstream.LotteryDrawContext(ctx, a)
		if err != nil {
			log.Printf("streak-bonus %s: draw: %v", logfmt.Label(a.UID, a.Nickname), err)
			return
		}
		log.Printf("streak-bonus %s: 🎲 第%d抽 %s", logfmt.Label(a.UID, a.Nickname), i+1, compactJSON(raw))
	}
	if chances > 0 {
		log.Printf("streak-bonus %s: 抽奖完成 %d 次", logfmt.Label(a.UID, a.Nickname), chances)
	}
}

// compactJSON 裁剪奖品载荷（日志单行可读）。
func compactJSON(raw json.RawMessage) string {
	s := string(raw)
	if len(s) > 220 {
		return s[:220] + "…"
	}
	return s
}

// makeupYesterday 昨日漏签且有补签卡时自动补签（保住连登连续天数）。
// 无卡 / 无漏签 / 查询失败均静默（不影响主流程）。
func (s *Scheduler) makeupYesterday(a *auth.Auth) {
	s.makeupYesterdayContext(context.Background(), a)
}

func (s *Scheduler) makeupYesterdayContext(ctx context.Context, a *auth.Auth) {
	if ctx.Err() != nil {
		return
	}
	missed, err := s.cfg.Upstream.HeatmapYesterdayMissedContext(ctx, a)
	if err != nil || !missed {
		return
	}
	if ctx.Err() != nil {
		return
	}
	full, err := s.cfg.Upstream.GrowthStreakFullContext(ctx, a)
	if err != nil || full.MakeupCards.Balance <= 0 {
		return
	}
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	if ctx.Err() != nil {
		return
	}
	if err := s.cfg.Upstream.UseMakeupCardContext(ctx, a, yesterday); err != nil {
		log.Printf("streak-bonus %s: 补签 %s 失败: %v", logfmt.Label(a.UID, a.Nickname), yesterday, err)
		return
	}
	log.Printf("streak-bonus %s: ★ 已用补签卡补签 %s（保连登）", logfmt.Label(a.UID, a.Nickname), yesterday)
}
