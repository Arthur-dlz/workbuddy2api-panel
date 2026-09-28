// Package livecfg 运行期可变配置的并发安全持有者。
package livecfg

import (
	"strings"
	"sync/atomic"
	"time"
)

// ModelRoute 单个模型路由规则（中转站级）。
type ModelRoute struct {
	ID          string   `json:"id"`                    // 外部客户端请求模型名，如 "deepseek-chat"
	Target      string   `json:"target"`                // 上游映射目标模型名，如 "tencent-code-v3"
	Fallbacks   []string `json:"fallbacks,omitempty"`   // 备用模型降级列表，如 ["glm-5.2"]
	Enabled     bool     `json:"enabled"`               // 是否启用
	Reasoning   bool     `json:"reasoning"`             // 是否开启深度思考（reasoning_content）
	Effort      string   `json:"effort,omitempty"`      // 思考档位: "low", "medium", "high"
	MaxInFlight int      `json:"max_in_flight"`         // 单模型并发在途上限（0 为不限）
	RPM         int      `json:"rpm,omitempty"`         // 单模型每分钟请求数限流（0 为不限）
	Description string   `json:"description,omitempty"` // 描述备注
}

// TokenConfig 独立 API Key / 令牌配置（中转站级）。
type TokenConfig struct {
	Key       string   `json:"key"`                  // 令牌密钥 (如 sk-xxx)
	Name      string   `json:"name"`                 // 令牌名称 / 备注
	Enabled   bool     `json:"enabled"`              // 是否启用
	BindModel string   `json:"bind_model,omitempty"` // 唯一强绑定的上游真实模型 (如 "deepseek-v4-pro")
	Models    []string `json:"models,omitempty"`     // 允许访问的模型白名单 (空或包含 "*" 表示所有)
	RPM       int      `json:"rpm,omitempty"`        // 独立限流（0 不限）
}

// Snapshot 一次读取的不可变配置视图。
type Snapshot struct {
	APIKey               string            // 网关/面板共同鉴权密钥；空 = 不鉴权
	SoftCooldown         time.Duration     // 429 软冷却基数（<=0 时调用方回退内置默认）
	SanitizeFingerprints bool              // 出站请求体指纹脱敏
	Models               map[string]string // 模型别名字典映射 (如 deepseek-chat -> tencent-code-v3)
	ModelRoutes          []ModelRoute      // 中转站级模型路由规则
	Tokens               []TokenConfig     // 多 API 令牌配置
}

// ResolveModelAlias 从快照中的别名字典或路由列表查找目标模型。
func (s Snapshot) ResolveModelAlias(model string) (string, bool) {
	if r, ok := s.FindRoute(model); ok {
		if !r.Enabled {
			return "", false
		}
		return r.Target, true
	}
	if len(s.Models) == 0 || model == "" {
		return "", false
	}
	target, ok := s.Models[model]
	return target, ok
}

// FindRoute 查找指定外部模型名称的路由规则。
func (s Snapshot) FindRoute(model string) (ModelRoute, bool) {
	for _, r := range s.ModelRoutes {
		if r.ID == model {
			return r, true
		}
	}
	if target, ok := s.Models[model]; ok {
		isReasoner := strings.Contains(strings.ToLower(model), "reasoner") ||
			strings.Contains(strings.ToLower(model), "-r1")
		return ModelRoute{
			ID:        model,
			Target:    target,
			Enabled:   true,
			Reasoning: isReasoner,
			Effort:    "medium",
		}, true
	}
	return ModelRoute{}, false
}

// FindToken 查找指定密钥的令牌配置。
func (s Snapshot) FindToken(providedKey string) (TokenConfig, bool) {
	for _, tk := range s.Tokens {
		if tk.Key == providedKey {
			return tk, true
		}
	}
	return TokenConfig{}, false
}

// ValidateAuthToken 校验 API 鉴权并返回是否有权调用该模型以及匹配的 TokenConfig。
func (s Snapshot) ValidateAuthToken(providedKey, model string) (valid bool, modelAllowed bool, token *TokenConfig) {
	// 1. 无需鉴权
	if s.APIKey == "" && len(s.Tokens) == 0 {
		return true, true, nil
	}

	// 2. 主密钥优先匹配
	if s.APIKey != "" && providedKey == s.APIKey {
		return true, true, nil
	}

	// 3. 多令牌匹配
	for i := range s.Tokens {
		tk := &s.Tokens[i]
		if tk.Enabled && tk.Key == providedKey {
			// 若配置了唯一强绑定的上游真实模型，则无需白名单限制，始终允许调用
			if tk.BindModel != "" {
				return true, true, tk
			}
			if len(tk.Models) == 0 {
				return true, true, tk
			}
			for _, m := range tk.Models {
				if m == "*" || m == model {
					return true, true, tk
				}
			}
			return true, false, tk
		}
	}

	return false, false, nil
}

// ValidateAuth 校验 API 鉴权并返回是否有权调用该模型。
func (s Snapshot) ValidateAuth(providedKey, model string) (valid bool, modelAllowed bool, tokenName string) {
	valid, allowed, tk := s.ValidateAuthToken(providedKey, model)
	if !valid {
		return false, false, ""
	}
	if tk == nil {
		if s.APIKey == "" {
			return true, true, "免鉴权"
		}
		return true, true, "主密钥"
	}
	return valid, allowed, tk.Name
}

// Holder 原子持有当前快照。
type Holder struct {
	p atomic.Pointer[Snapshot]
}

// New 以初始快照构建。
func New(s Snapshot) *Holder {
	h := &Holder{}
	h.Store(s)
	return h
}

// Load 返回当前快照（Holder 为 nil 或从未 Store 时返回零值快照，调用方无需判空）。
func (h *Holder) Load() Snapshot {
	if h == nil {
		return Snapshot{}
	}
	if s := h.p.Load(); s != nil {
		return *s
	}
	return Snapshot{}
}

// Store 整体替换快照。
func (h *Holder) Store(s Snapshot) { h.p.Store(&s) }
