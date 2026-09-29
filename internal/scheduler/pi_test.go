package scheduler

import (
	"fmt"
	"testing"
	"time"
)

func TestPiDigitsIntegrity(t *testing.T) {
	if len(PiDigits) != 2000 {
		t.Fatalf("PiDigits 长度必须为 2000，当前为 %d", len(PiDigits))
	}
	// 验证前若干位和后若干位
	if !startsWith(PiDigits, "1415926535") {
		t.Errorf("PiDigits 前缀不符合预期: %s", PiDigits[:10])
	}
	if !endsWith(PiDigits, "802759009") {
		t.Errorf("PiDigits 后缀不符合预期: %s", PiDigits[len(PiDigits)-9:])
	}
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func TestPiCalculateStepCoprimeTo2000(t *testing.T) {
	// 测试大量的 hash 值，验证生成的 step 与 2000 互质 (GCD == 1)
	for h := uint32(0); h < 5000; h++ {
		step := CalculateStep(h)
		if step <= 0 {
			t.Fatalf("h=%d step must be positive, got %d", h, step)
		}
		if GCD(step, 2000) != 1 {
			t.Fatalf("h=%d step=%d 与 2000 不互质: gcd=%d", h, step, GCD(step, 2000))
		}
		if step%2 == 0 {
			t.Fatalf("h=%d step=%d 不能是偶数", h, step)
		}
		if step%5 == 0 {
			t.Fatalf("h=%d step=%d 不能是 5 的倍数", h, step)
		}
	}
}

func TestPiScheduleFullPeriod(t *testing.T) {
	// 针对不同 UID，验证遍历 2000 次能够恰好完全覆盖 [0, 1999] 的全部索引且无重复
	uids := []string{"user_1001", "user_1002", "wx_99999", "copilot_test_account_42"}
	for _, uid := range uids {
		sched := NewPiSchedule(uid)
		visited := make(map[int]bool, 2000)
		startCursor := sched.Cursor()

		for i := 0; i < 2000; i++ {
			c := sched.Cursor()
			if visited[c] {
				t.Fatalf("uid=%s 在第 %d 步重复访问游标 %d (周期退化)", uid, i, c)
			}
			visited[c] = true
			sched.NextDelay()
		}

		if len(visited) != 2000 {
			t.Fatalf("uid=%s 遍历 2000 步后只覆盖了 %d 个游标，未达满周期", uid, len(visited))
		}

		// 第 2000 步后游标应回到起点
		if sched.Cursor() != startCursor {
			t.Fatalf("uid=%s 2000 步后游标=%d, 期望回到起点=%d", uid, sched.Cursor(), startCursor)
		}
	}
}

func TestPiScheduleDelays(t *testing.T) {
	sched := NewPiSchedule("test_account_uid_123")

	// 验证 InitialDelay 在 [5s, 300s] 内
	for i := 0; i < 50; i++ {
		s := NewPiSchedule(fmt.Sprintf("account_%d", i))
		d := s.InitialDelay()
		if d < 5*time.Second || d > 300*time.Second {
			t.Fatalf("InitialDelay %v out of [5s, 300s]", d)
		}
	}

	// 验证 NextDelay 在 [300s, 600s] 内
	for i := 0; i < 100; i++ {
		d := sched.NextDelay()
		if d < 300*time.Second || d > 600*time.Second {
			t.Fatalf("NextDelay %v out of [300s, 600s]", d)
		}
	}

	// 验证 NextDelayForInterval
	customBase := 10 * time.Minute
	dCustom := sched.NextDelayForInterval(customBase)
	if dCustom < customBase || dCustom > 2*customBase {
		t.Fatalf("NextDelayForInterval %v out of expected range", dCustom)
	}

	// interval <= 0 应返回 0
	if sched.NextDelayForInterval(0) != 0 {
		t.Fatalf("NextDelayForInterval(0) must return 0")
	}
}
