// blackcat.go 夜猫子任务（black_cat）+ 新手礼包/补偿 API。
//
// 判据（WorkBuddy-Daily 项目实测口径 + 本网关验证）：black_cat 要求在
// **23:00–08:00（本地时区）窗口内**完成 3 次 glm-5.2 对话并上报 chat 事件链；
// 窗口外行为不计分。真实对话走网关既有 ChatStream（glm-5.2），事件链用
// ReportChatActivityModel（chat_5 同款上报形状）。
package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
)

// InNightWindow 当前是否处于夜猫子计数窗口（23:00–08:00 本地时区）。
func InNightWindow(now time.Time) bool {
	h := now.Hour()
	return h >= 23 || h < 8
}

// BlackcatNeed 查 black_cat 任务剩余差额（需要再完成几次对话）。
// 任务不存在返回 0（无可做）；拉取失败返回错误。
func (c *Client) BlackcatNeed(a *auth.Auth) (int64, error) {
	return c.BlackcatNeedContext(context.Background(), a)
}

func (c *Client) BlackcatNeedContext(ctx context.Context, a *auth.Auth) (int64, error) {
	tasks, err := c.ListTasksContext(ctx, a)
	if err != nil {
		return 0, err
	}
	for _, t := range tasks {
		if t.TaskCode == "black_cat" {
			if t.Claimed || t.Current >= t.Target {
				return 0, nil
			}
			return t.Target - t.Current, nil
		}
	}
	return 0, nil
}

// RunNightChats 夜猫子：发 need 次 glm-5.2 真实对话（读干流）并上报事件链。
// 返回成功次数。对话内容极短（1+1），消耗可忽略。
func (c *Client) RunNightChats(a *auth.Auth, need int) (int64, error) {
	return c.RunNightChatsContext(context.Background(), a, need)
}

func (c *Client) RunNightChatsContext(ctx context.Context, a *auth.Auth, need int) (int64, error) {
	var ok int64
	for i := 0; i < need; i++ {
		if err := ctx.Err(); err != nil {
			return ok, err
		}
		body, _ := json.Marshal(map[string]any{
			"model":    "glm-5.2",
			"messages": []map[string]any{{"role": "user", "content": "1+1等于几？直接回答。"}},
			"stream":   true,
		})
		rc, status, respBody, err := c.ChatStreamContext(ctx, a, body, "", ChatMeta{})
		if err != nil || status >= 400 {
			if rc != nil {
				rc.Close()
			}
			return ok, fmt.Errorf("第 %d 次对话失败: http=%d err=%v body=%.120s", i+1, status, err, respBody)
		}
		_, copyErr := io.Copy(io.Discard, io.LimitReader(rc, 1<<20))
		rc.Close()
		if copyErr != nil {
			return ok, fmt.Errorf("第 %d 次对话读取失败: %w", i+1, copyErr)
		}
		if err := c.ReportChatActivityModelContext(ctx, a, fmt.Sprintf("wb2api-night-%d-%d", time.Now().UnixMilli(), i), "", "glm-5.2", "GLM-5.2"); err != nil {
			return ok, fmt.Errorf("第 %d 次上报失败: %w", i+1, err)
		}
		ok++
		timer := time.NewTimer(4 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ok, ctx.Err()
		case <-timer.C:
		}
	}
	return ok, nil
}

// ClaimGift 领取新手礼包（每号一次，已领返回业务错误）。
func (c *Client) ClaimGift(a *auth.Auth) (int64, error) {
	return c.ClaimGiftContext(context.Background(), a)
}

func (c *Client) ClaimGiftContext(ctx context.Context, a *auth.Auth) (int64, error) {
	data, err := c.billingJSONContext(ctx, a, http.MethodPost, "/billing/meter/claim-gift", map[string]any{})
	if err != nil {
		return 0, err
	}
	var resp struct {
		Credit int64 `json:"credit"`
	}
	_ = json.Unmarshal(data, &resp)
	return resp.Credit, nil
}

// ClaimCompensation 领取活动补偿（有则领，无则业务错误）。
func (c *Client) ClaimCompensation(a *auth.Auth) (int64, error) {
	return c.ClaimCompensationContext(context.Background(), a)
}

func (c *Client) ClaimCompensationContext(ctx context.Context, a *auth.Auth) (int64, error) {
	data, err := c.billingJSONContext(ctx, a, http.MethodPost, "/billing/meter/claim-compensation", map[string]any{})
	if err != nil {
		return 0, err
	}
	var resp struct {
		Credit int64 `json:"credit"`
	}
	_ = json.Unmarshal(data, &resp)
	return resp.Credit, nil
}

// HeatmapYesterdayMissed 检查昨日是否漏签（heatmap cell score==0）。
func (c *Client) HeatmapYesterdayMissed(a *auth.Auth) (bool, error) {
	return c.HeatmapYesterdayMissedContext(context.Background(), a)
}

func (c *Client) HeatmapYesterdayMissedContext(ctx context.Context, a *auth.Auth) (bool, error) {
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	data, err := c.growthJSONContext(ctx, a, http.MethodGet, "/activity/growth/heatmap", nil)
	if err != nil {
		return false, err
	}
	var resp struct {
		Cells []struct {
			Date  string `json:"date"`
			Score int    `json:"score"`
		} `json:"cells"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return false, err
	}
	for _, cell := range resp.Cells {
		if len(cell.Date) >= 10 && cell.Date[:10] == yesterday {
			return cell.Score == 0, nil
		}
	}
	return false, nil
}

// UseMakeupCard 对指定日期使用补签卡（保住连登连续天数；无卡返回业务错误）。
func (c *Client) UseMakeupCard(a *auth.Auth, date string) error {
	return c.UseMakeupCardContext(context.Background(), a, date)
}

func (c *Client) UseMakeupCardContext(ctx context.Context, a *auth.Auth, date string) error {
	_, err := c.growthJSONContext(ctx, a, http.MethodPost, "/activity/growth/makeup-cards/use",
		map[string]any{"target_date": date})
	return err
}
