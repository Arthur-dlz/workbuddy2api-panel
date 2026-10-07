// sse.go 处理上游 SSE 流：聚合成单个 OpenAI 响应，或透传给客户端。
package upstream

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// errEmptyStream 上游返回 200 但没有有效 SSE 数据帧（空流/只有注释/[DONE]）。
// 用哨兵错误替代裸 fmt.Errorf：StreamHint 的调用方（handler 流式路径）需要区分
// 「上游空流」与「客户端断连写失败」——空流是上游缺陷，应记 502 观测；写失败是
// 客户端已走，日志口径不同。Aggregate 与 StreamHint 共用同一哨兵（errors.Is 判定）。
var errEmptyStream = errors.New("upstream stream contained no valid data events")

// IsEmptyStreamError 报告错误是否为「上游空流」（无有效 SSE 帧）——供 handler
// 在流式路径把空流记为失败观测（HTTP 头已发出只能 200，但日志/状态应收敛到
// upstream_parse 同语义），与客户端断连类错误区分。
func IsEmptyStreamError(err error) bool { return errors.Is(err, errEmptyStream) }

// errStreamTruncated 上游流中途非正常截断（收到有效帧但未见 finish_reason 也未见 [DONE] 就 EOF/断开）。
var errStreamTruncated = errors.New("upstream stream truncated prematurely without [DONE]")

// IsStreamTruncatedError 报告错误是否为「上游流中途截断」——供 handler 在流式与非流式路径识别。
func IsStreamTruncatedError(err error) bool { return errors.Is(err, errStreamTruncated) }

// errUpstreamInStreamError 上游在 200 流内下发了 error 帧（例如 6004 限流、审核拦截、账号异常）。
var errUpstreamInStreamError = errors.New("upstream returned error event in stream")

// IsUpstreamInStreamError 报告错误是否为「上游流内返回 error 帧」——供 handler 在流式与非流式路径识别并拒绝假成功。
func IsUpstreamInStreamError(err error) bool { return errors.Is(err, errUpstreamInStreamError) }

type sseEventReader struct {
	br     *bufio.Reader
	skipLF bool
}

func newSSEEventReader(r io.Reader) *sseEventReader {
	return &sseEventReader{br: bufio.NewReaderSize(r, 64*1024)}
}

// readDataEvent returns one complete SSE data event. Per the SSE format, one
// optional space after ':' is removed and multiple data fields are joined with
// '\n'. Incomplete events at EOF or a read error are discarded.
func (r *sseEventReader) readDataEvent() (string, bool, error) {
	var data []string
	for {
		line, complete, err := r.readLine()
		if err != nil {
			return "", false, err
		}
		if !complete {
			return "", false, io.EOF
		}
		if line == "" {
			if len(data) == 0 {
				continue
			}
			return strings.Join(data, "\n"), true, nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, hasValue := strings.Cut(line, ":")
		if !hasValue {
			value = ""
		}
		if field == "data" {
			data = append(data, strings.TrimPrefix(value, " "))
		}
	}
}

func (r *sseEventReader) readLine() (string, bool, error) {
	var line strings.Builder
	if r.skipLF {
		r.skipLF = false
		if b, err := r.br.ReadByte(); err == nil && b != '\n' {
			if err := r.br.UnreadByte(); err != nil {
				return "", false, err
			}
		} else if err != nil && err != io.EOF {
			return "", false, err
		}
	}
	for {
		b, err := r.br.ReadByte()
		if err != nil {
			if err == io.EOF && line.Len() > 0 {
				return "", false, nil
			}
			return "", false, err
		}
		if b == '\n' {
			return line.String(), true, nil
		}
		if b == '\r' {
			r.skipLF = true
			return line.String(), true, nil
		}
		line.WriteByte(b)
	}
}

func hasMeaningfulChunk(chunk map[string]any, stream bool) bool {
	if hasUpstreamError(chunk) {
		return true
	}
	if choices, ok := chunk["choices"].([]any); ok {
		for _, item := range choices {
			choice, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if delta, ok := choice["delta"].(map[string]any); ok && hasSupportedMessageFields(delta, stream) {
				return true
			}
			if !stream {
				if message, ok := choice["message"].(map[string]any); ok && hasSupportedMessageFields(message, false) {
					return true
				}
			}
		}
	}
	if _, ok := primaryFinishReason(chunk); ok {
		return true
	}
	usage, ok := chunk["usage"].(map[string]any)
	return ok && len(usage) > 0
}

func hasSupportedMessageFields(message map[string]any, stream bool) bool {
	keys := []string{"role", "content", "reasoning_content", "refusal"}
	for _, key := range keys {
		if value, ok := message[key].(string); ok && value != "" {
			return true
		}
	}
	if calls, ok := message["tool_calls"].([]any); ok {
		for _, item := range calls {
			call, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if id, ok := call["id"].(string); ok && id != "" {
				return true
			}
			if typ, ok := call["type"].(string); ok && typ != "" {
				return true
			}
			if function, ok := call["function"].(map[string]any); ok {
				if name, ok := function["name"].(string); ok && name != "" {
					return true
				}
				if arguments, ok := function["arguments"].(string); ok && arguments != "" {
					return true
				}
			}
		}
	}
	if stream {
		call, ok := message["function_call"]
		if !ok || call == nil {
			return false
		}
		fields, ok := call.(map[string]any)
		if !ok {
			return true
		}
		name, _ := fields["name"].(string)
		arguments, _ := fields["arguments"].(string)
		if name != "" || arguments != "" {
			return true
		}
	}
	return false
}

func hasUpstreamError(chunk map[string]any) bool {
	v, ok := chunk["error"]
	return ok && v != nil
}

func primaryFinishReason(chunk map[string]any) (string, bool) {
	choices, ok := chunk["choices"].([]any)
	if !ok || len(choices) == 0 {
		return "", false
	}
	var positionalPrimary map[string]any
	hasExplicitIndex := false
	for position, item := range choices {
		choice, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if index, present := choice["index"]; present {
			hasExplicitIndex = true
			n, ok := index.(float64)
			if !ok || n != 0 {
				continue
			}
			return recognizedFinishReason(choice)
		}
		if position == 0 {
			positionalPrimary = choice
		}
	}
	// Legacy upstream frames without indexes use array position as choice index.
	// Once any explicit index is present, it is authoritative; a secondary-only
	// frame must not be mistaken for primary completion.
	if hasExplicitIndex || positionalPrimary == nil {
		return "", false
	}
	return recognizedFinishReason(positionalPrimary)
}

func recognizedFinishReason(choice map[string]any) (string, bool) {
	reason, ok := choice["finish_reason"].(string)
	switch reason {
	case "stop", "length", "tool_calls", "content_filter", "function_call":
		return reason, ok
	default:
		return "", false
	}
}

func trustedFinishReason(reason string) bool {
	switch reason {
	case "stop", "length", "tool_calls":
		return true
	default:
		return false
	}
}

type aggregateSecondaryChoice struct {
	role             string
	content          strings.Builder
	reasoning        strings.Builder
	refusal          strings.Builder
	functionCallName string
	functionCallArgs string
	gotContent       bool
	finishReason     string
	toolCalls        map[int]map[string]any
	toolOrder        []int
}

func newAggregateSecondaryChoice() *aggregateSecondaryChoice {
	return &aggregateSecondaryChoice{role: "assistant", toolCalls: map[int]map[string]any{}}
}

func (c *aggregateSecondaryChoice) mergeTools(tcs []any) {
	for position, item := range tcs {
		call, ok := item.(map[string]any)
		if !ok {
			continue
		}
		idx := position
		if n, ok := call["index"].(float64); ok {
			idx = int(n)
		}
		merged, exists := c.toolCalls[idx]
		if !exists {
			merged = map[string]any{"index": idx}
			c.toolCalls[idx] = merged
			c.toolOrder = append(c.toolOrder, idx)
		}
		mergeToolCallDelta(merged, call)
	}
}

// Aggregate 读取完整 SSE 流，聚合 delta.content 为单个 OpenAI chat.completion 响应。
// 分片/半行由 bufio.Reader.ReadString 处理；遇到 "data: [DONE]" 结束。
// tool_calls 以流式 delta 到达（按 index 合并：首片带 id/type/name，后续只带 arguments 片段）。
func Aggregate(r io.Reader) (map[string]any, error) {
	br := newSSEEventReader(r)
	var (
		id, model      string
		created        float64
		content        strings.Builder
		reasoning      strings.Builder
		refusal        strings.Builder
		functionCall   = map[string]string{}
		role           = "assistant"
		finishReason   = "stop"
		usage          map[string]any
		gotAnyContent  bool
		validEvents    int
		sawDone        bool // 上游显式发过 data: [DONE]（正常收尾）
		choiceSeen     = map[int]bool{}
		choiceFinished = map[int]bool{}
		secondary      = map[int]*aggregateSecondaryChoice{}
		toolCalls      = map[int]map[string]any{}
		toolOrder      []int
		// toolSeq 缺 index 的 tool_call 的分配序号源：跨帧延续「最近分配」槽位，
		// 同帧内递增（见 mergeToolCallsChunk 注释）。
		toolSeq int
		// idIndex id → 已分配的 index：跨帧持续（延续碎片的 id 常出现在后续帧），
		// 供缺 index 时按 id 归位既有调用（见 mergeToolCallsChunk 注释）。
		idIndex = map[string]int{}
	)
	choicesComplete := func() bool {
		if len(choiceSeen) == 0 || !choiceFinished[0] {
			return false
		}
		for index := range choiceSeen {
			if !choiceFinished[index] {
				return false
			}
		}
		return true
	}
	// appendContent 是「已取到正文」（gotAnyContent latch）的唯一写入点：delta 与
	// message 两路 content 都必须经此并入，规约只有一份（issue #142）——
	//   S1 空串不算「已取到正文」、不占 latch 名额：OpenAI 风格 role-only 首帧
	//      （delta.content=""）和整条空 message 帧是常态帧，若空串置位 latch，
	//      后续真正文会被 message 回退分支的 !gotAnyContent 守卫静默拒绝；
	//   S2 空串本身也无可追加字节，跳过 WriteString 与追加语义自洽。
	appendContent := func(txt string) {
		if txt == "" {
			return
		}
		content.WriteString(txt)
		gotAnyContent = true
	}
	// mergeToolCallsChunk 把一段 tool_calls 数组按 index 合并进累计表。
	// delta（流式分片，按 index 累积）与 message（非 delta 整条）共用同一合并逻辑，
	// 保证「上游给的身份/函数名不丢、arguments 拼接语义一致」。
	//
	// index 缺失兼容：OpenAI 规范要求 delta 帧的 tool_call 带 index（标记分片归属），
	// 但部分上游省略它。此前缺 index 一律归 0——多调用场景下不同 call 被合并进同一
	// index 槽，arguments 串联、name 互相覆盖（数据污染）。修法按「id 优先、lastIdx 兜底」：
	//   - 带 index → 按 index 累积（合规形态，零改动）；
	//   - 缺 index 带 id 且 id 已见过 → 延续该 id 所在 index；
	//   - 缺 index 带 id 且 id 是新的 → 开新序号（多调用不合并）；
	//   - 缺 index 无 id → 追加到最近收到碎片的 index（单个调用的延续分片无 id
	//     是标准形态），无既往则开新号。
	// 带 index 的碎片照常按 index 累积，不受影响。
	// nextToolIndex 分配下一个不冲突的缺 index 序号：从 toolSeq 起递增跳过既有
	// index（合规流的 index 是 0..N-1，缺 index 的补位不能覆盖它们）。
	nextToolIndex := func() int {
		for {
			idx := toolSeq
			toolSeq++
			if _, used := toolCalls[idx]; !used {
				return idx
			}
		}
	}
	mergeToolCallsChunk := func(tcs []any) {
		for _, tc := range tcs {
			call, ok := tc.(map[string]any)
			if !ok {
				continue
			}
			idx := -1
			if v, ok := call["index"].(float64); ok {
				idx = int(v)
			} else if cid, _ := call["id"].(string); cid != "" {
				if mid, seen := idIndex[cid]; seen {
					idx = mid // 该 id 已归位过：延续既有调用（跨帧有效）
				} else {
					idx = nextToolIndex()
				}
			} else if len(toolOrder) > 0 {
				idx = toolOrder[len(toolOrder)-1] // 无 id 碎片：延续最近调用
			} else {
				idx = nextToolIndex()
			}
			merged, seen := toolCalls[idx]
			if !seen {
				merged = map[string]any{"index": idx}
				toolCalls[idx] = merged
				toolOrder = append(toolOrder, idx)
			}
			if cid, _ := call["id"].(string); cid != "" {
				idIndex[cid] = idx
			}
			if cid, _ := merged["id"].(string); cid != "" {
				idIndex[cid] = idx
			}
			mergeToolCallDelta(merged, call)
		}
	}
	// mergeMessageFields 把非 delta 的完整 message 内容并入聚合（message 是整条下发，
	// 非流式拼接，content 只取一次）。role/reasoning_content/tool_calls 与 delta 分支
	// 同构透出；content 同样置 gotAnyContent，与 delta 路径的 latch 语义一致
	// （一帧整条 message 之后，后续 delta 帧不重复追加）。
	mergeMessageFields := func(msg map[string]any) {
		if r2, ok := msg["role"].(string); ok && r2 != "" {
			role = r2
		}
		if txt, ok := msg["content"].(string); ok {
			appendContent(txt)
		}
		if rc, ok := msg["reasoning_content"].(string); ok {
			reasoning.WriteString(rc)
		}
		if rf, ok := msg["refusal"].(string); ok {
			refusal.WriteString(rf)
		}
		if fc, ok := msg["function_call"].(map[string]any); ok {
			if n, ok := fc["name"].(string); ok && n != "" {
				functionCall["name"] = n
			}
			if a, ok := fc["arguments"].(string); ok {
				functionCall["arguments"] += a
			}
		}
		if tcs, ok := msg["tool_calls"].([]any); ok {
			mergeToolCallsChunk(tcs)
		}
	}
	for {
		payload, complete, err := br.readDataEvent()
		if err == io.EOF {
			break
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil, fmt.Errorf("%w: %w", errStreamTruncated, err)
			}
			if choicesComplete() {
				break
			}
			return nil, fmt.Errorf("%w: %w", errStreamTruncated, err)
		}
		if !complete {
			continue
		}
		if payload == "[DONE]" {
			// 上游显式结束：停止读取，DONE 之后的任何数据一律忽略。
			sawDone = true
			break
		} else {
			var chunk map[string]any
			if json.Unmarshal([]byte(payload), &chunk) == nil && chunk != nil {
				// 上游如果在流内下发了错误帧（如 6004、审核拦截），非流式直接报错，不能合成空成功响应
				if hasUpstreamError(chunk) {
					errVal := chunk["error"]
					errBytes, _ := json.Marshal(errVal)
					return nil, fmt.Errorf("%w: %s", errUpstreamInStreamError, string(errBytes))
				}
				if !hasMeaningfulChunk(chunk, false) {
					continue
				}
				// 有效事件计数：只接受带 choice 或 usage 内容的非空 chunk。
				validEvents++
				if v, ok := chunk["id"].(string); ok && id == "" {
					id = v
				}
				if v, ok := chunk["model"].(string); ok && model == "" {
					model = v
				}
				if v, ok := chunk["created"].(float64); ok && created == 0 {
					created = v
				}
				if u, ok := chunk["usage"].(map[string]any); ok {
					usage = u
				}
				if ch, ok := chunk["choices"].([]any); ok {
					for position, ci := range ch {
						c, _ := ci.(map[string]any)
						if c == nil {
							continue
						}
						choiceIndex := position
						if n, ok := c["index"].(float64); ok {
							choiceIndex = int(n)
						}
						choiceSeen[choiceIndex] = true
						if fr, ok := c["finish_reason"].(string); ok && trustedFinishReason(fr) {
							choiceFinished[choiceIndex] = true
						}
						if choiceIndex == 0 {
							if fr, ok := c["finish_reason"].(string); ok {
								switch fr {
								case "stop", "length", "tool_calls", "content_filter", "function_call":
									finishReason = fr
								}
							}
						} else {
							state := secondary[choiceIndex]
							if state == nil {
								state = newAggregateSecondaryChoice()
								secondary[choiceIndex] = state
							}
							if fr, ok := c["finish_reason"].(string); ok {
								state.finishReason = fr
							}
							if delta, ok := c["delta"].(map[string]any); ok {
								if v, ok := delta["role"].(string); ok && v != "" {
									state.role = v
								}
								if v, ok := delta["content"].(string); ok && v != "" {
									state.content.WriteString(v)
									state.gotContent = true
								}
								if v, ok := delta["reasoning_content"].(string); ok {
									state.reasoning.WriteString(v)
								}
								if v, ok := delta["refusal"].(string); ok {
									state.refusal.WriteString(v)
								}
								if fc, ok := delta["function_call"].(map[string]any); ok {
									if n, ok := fc["name"].(string); ok && n != "" {
										state.functionCallName = n
									}
									if a, ok := fc["arguments"].(string); ok {
										state.functionCallArgs += a
									}
								}
								if calls, ok := delta["tool_calls"].([]any); ok {
									state.mergeTools(calls)
								}
							}
							if msg, ok := c["message"].(map[string]any); ok && !state.gotContent {
								if v, ok := msg["role"].(string); ok && v != "" {
									state.role = v
								}
								if v, ok := msg["content"].(string); ok {
									state.content.WriteString(v)
									state.gotContent = v != ""
								}
								if v, ok := msg["reasoning_content"].(string); ok {
									state.reasoning.WriteString(v)
								}
								if v, ok := msg["refusal"].(string); ok {
									state.refusal.WriteString(v)
								}
								if fc, ok := msg["function_call"].(map[string]any); ok {
									if n, ok := fc["name"].(string); ok && n != "" {
										state.functionCallName = n
									}
									if a, ok := fc["arguments"].(string); ok {
										state.functionCallArgs += a
									}
								}
								if calls, ok := msg["tool_calls"].([]any); ok {
									state.mergeTools(calls)
								}
							}
							continue
						}
						if delta, ok := c["delta"].(map[string]any); ok {
							if r2, ok := delta["role"].(string); ok && r2 != "" {
								role = r2
							}
							if txt, ok := delta["content"].(string); ok {
								appendContent(txt)
							}
							if rc, ok := delta["reasoning_content"].(string); ok {
								reasoning.WriteString(rc)
							}
							if rf, ok := delta["refusal"].(string); ok {
								refusal.WriteString(rf)
							}
							if fc, ok := delta["function_call"].(map[string]any); ok {
								if n, ok := fc["name"].(string); ok && n != "" {
									functionCall["name"] = n
								}
								if a, ok := fc["arguments"].(string); ok {
									functionCall["arguments"] += a
								}
							}
							if tcs, ok := delta["tool_calls"].([]any); ok {
								mergeToolCallsChunk(tcs)
							}
						}
						// 有的上游把完整消息放在 message 里（非 delta）：
						// 整条并入（role/content/reasoning_content/tool_calls），
						// 与 delta 分支同构。delta 已取过正文（gotAnyContent）则跳过
						// （避免与 delta 路径重复拼接——PR #134 的 latch 语义）。
						if msg, ok := c["message"].(map[string]any); ok && !gotAnyContent {
							mergeMessageFields(msg)
						}
					}
				}
			}
		}
	}
	if validEvents == 0 {
		// 上游返回 200 但没有任何有效数据事件（空流/只有 [DONE]/只有注释行）：
		// 不再合成空 content 的假成功响应，直接报错，由 handler 映射为 502 upstream_parse。
		return nil, errEmptyStream
	}
	if len(choiceSeen) == 0 {
		return nil, errEmptyStream
	}
	if !sawDone && !choicesComplete() {
		// 上游在吐流中途断开（既未见 finish_reason 也未见 [DONE]）：
		// 说明流被异常截断，直接报错，由 handler 识别并映射为 502 upstream_parse。
		return nil, errStreamTruncated
	}
	if id == "" {
		id = fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	}
	if created == 0 {
		created = float64(time.Now().Unix())
	}
	message := map[string]any{
		"role":    role,
		"content": content.String(),
	}
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	if refusal.Len() > 0 {
		message["refusal"] = refusal.String()
	}
	if len(functionCall) > 0 {
		message["function_call"] = functionCall
	}
	if len(toolOrder) > 0 {
		sort.Ints(toolOrder)
		calls := make([]map[string]any, 0, len(toolOrder))
		for _, idx := range toolOrder {
			calls = append(calls, toolCalls[idx])
		}
		// P1b：流被截断时 tool_call 的 arguments 是残缺 JSON（解析失败），不把脏参数
		// 交给客户端——残留分片会被客户端解析成非法 JSON 卡死会话。截断的两个来源：
		//   - finish_reason=="length"（模型因 max_tokens 提前中止）；
		//   - 上游连接中断（EOF 收尾但未发 data: [DONE]，sawDone=false）。
		// 完整参数原样保留（正例零改动）；空参数（无参工具）不是截断，同样保留。
		// 此前只认 finish_reason=="length"，EOF 截断的 tool_calls 残缺参数被原样下发。
		if finishReason == "length" || !sawDone {
			calls = dropTruncatedToolCalls(calls)
		}
		if len(calls) > 0 {
			message["tool_calls"] = calls
		}
	}
	choicesOut := make([]any, 0, len(choiceSeen))
	indices := make([]int, 0, len(choiceSeen))
	for index := range choiceSeen {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		if index == 0 {
			choicesOut = append(choicesOut, map[string]any{"index": 0, "message": message, "finish_reason": finishReason})
			continue
		}
		state := secondary[index]
		if state == nil {
			state = newAggregateSecondaryChoice()
		}
		secondaryMessage := map[string]any{"role": state.role, "content": state.content.String()}
		if state.reasoning.Len() > 0 {
			secondaryMessage["reasoning_content"] = state.reasoning.String()
		}
		if state.refusal.Len() > 0 {
			secondaryMessage["refusal"] = state.refusal.String()
		}
		if state.functionCallName != "" || state.functionCallArgs != "" {
			secondaryMessage["function_call"] = map[string]string{"name": state.functionCallName, "arguments": state.functionCallArgs}
		}
		if len(state.toolOrder) > 0 {
			sort.Ints(state.toolOrder)
			calls := make([]map[string]any, 0, len(state.toolOrder))
			for _, idx := range state.toolOrder {
				calls = append(calls, state.toolCalls[idx])
			}
			if state.finishReason == "length" || !sawDone {
				calls = dropTruncatedToolCalls(calls)
			}
			if len(calls) > 0 {
				secondaryMessage["tool_calls"] = calls
			}
		}
		fr := any(state.finishReason)
		if state.finishReason == "" {
			fr = finishReason
		}
		choicesOut = append(choicesOut, map[string]any{"index": index, "message": secondaryMessage, "finish_reason": fr})
	}
	resp := map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": int64(created),
		"model":   model,
		"choices": choicesOut,
	}
	if usage != nil {
		// OpenAI 非流式 usage 必含 total_tokens。上游若只发 prompt_tokens +
		// completion_tokens（部分上游末帧缺 total），网关合成补齐——否则严格按
		// schema 校验的客户端收不到 total_tokens。已有 total 或二者缺一不补
		// （不臆造：单边有值无法合成可信的 total）。
		resp["usage"] = normalizeUsageCacheAliases(ensureUsageTotal(usage))
	}
	return resp, nil
}

// ensureUsageTotal 在 usage 缺 total_tokens 但 prompt_tokens/completion_tokens 都在时
// 补齐 total = prompt + completion（通过新 map 合并，不修改原上游 map）。
// 任一缺失或已有 total 时原样返回。
func ensureUsageTotal(u map[string]any) map[string]any {
	if _, ok := u["total_tokens"]; ok {
		return u
	}
	pt, pok := num64(u["prompt_tokens"])
	ct, cok := num64(u["completion_tokens"])
	if !pok || !cok {
		return u
	}
	out := make(map[string]any, len(u)+1)
	for k, v := range u {
		out[k] = v
	}
	out["total_tokens"] = pt + ct
	return out
}

// num64 把 JSON number（float64/int64 均可）归一为 float64；非数字返回 ok=false。
//
// 注意与 payload.go 的类型 switch 语义不同：那边是翻译请求别名字段（非数字拒绝
// 整个请求），这边是聚合响应求和（非数字只跳过合成）。防后人合并两处。
func num64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	default:
		return 0, false
	}
}

// mergeToolCallDelta 把流式 tool_call 片段合并到累计对象：
// id/type/function.name 直覆盖（后续分片通常缺省），function.arguments 拼接。
func mergeToolCallDelta(merged, delta map[string]any) {
	if v, ok := delta["id"].(string); ok && v != "" {
		merged["id"] = v
	}
	if v, ok := delta["type"].(string); ok && v != "" {
		merged["type"] = v
	}
	df, _ := delta["function"].(map[string]any)
	if df == nil {
		return
	}
	mf, _ := merged["function"].(map[string]any)
	if mf == nil {
		mf = map[string]any{}
		merged["function"] = mf
	}
	if v, ok := df["name"].(string); ok && v != "" {
		mf["name"] = v
	}
	if v, ok := df["arguments"].(string); ok && v != "" {
		if prev, _ := mf["arguments"].(string); prev != "" {
			mf["arguments"] = prev + v
		} else {
			mf["arguments"] = v
		}
	}
}

// stripToolCallNames 收敛流式 tool_calls 的 name 语义为「每个 index 只出现一次」：
// 首片保留 function.name，同一 index 后续分片里的 name 键一律删除（无论上游是
// 空串还是重复非空串）。这是 OpenAI 官方流的真实形态——首帧带 name，后续帧只带
// arguments 片段、不再出现 name 键——因此是累加型与覆盖型客户端的共同祖先行为。
//
// 两类消费模型在该形态下同时正确：
//   - 累加型（官方 WorkBuddy/CodeBuddy `name += tc_function?.name || ""`）：
//     后续分片 name 键缺失 → 追加空串，累积 name 保持唯一，不再拼成 Bash×帧数（issue #82）。
//   - 覆盖型（hawklithm#2 / Grok Build `name ?? state.name` 或 `if (name) state.name = name`）：
//     后续分片 name 键缺失 → 保留已建好的首帧 name，不被空串意外清空。
//     键缺失是比空串更安全的形态：`??` 与 truthy 守卫对缺失键必然保留旧值，
//     而对空串，`??` 会误判为重设并清空工具名。
//
// seen 记录每个 index 是否已发过首片（与 name 是否非空无关）；删除是幂等的。
// 只动 function.name 键，id/type/arguments 原样透传。
type toolCallIdentity struct{ choice, index int }

func stripToolCallNames(obj map[string]any, seen map[toolCallIdentity]bool) {
	choices, _ := obj["choices"].([]any)
	for position, ci := range choices {
		c, _ := ci.(map[string]any)
		if c == nil {
			continue
		}
		delta, _ := c["delta"].(map[string]any)
		if delta == nil {
			continue
		}
		choiceIndex := position
		if n, ok := c["index"].(float64); ok {
			choiceIndex = int(n)
		}
		tcs, _ := delta["tool_calls"].([]any)
		for _, tci := range tcs {
			tc, _ := tci.(map[string]any)
			if tc == nil {
				continue
			}
			idx := 0
			if v, ok := tc["index"].(float64); ok {
				idx = int(v)
			}
			identity := toolCallIdentity{choice: choiceIndex, index: idx}
			if seen[identity] {
				// 已发过首片：删除本分片的 name 键（存在即删，幂等）。
				if fn, _ := tc["function"].(map[string]any); fn != nil {
					delete(fn, "name")
				}
				continue
			}
			// 首现：保留 name 键原样（上游首片通常带非空 name；空 name 也照发，
			// 与 OpenAI 对「首帧无 name」的容忍一致），随后分片统一删除。
			seen[identity] = true
		}
	}
}

// normalizeFrame 以 OpenAI 流式规范白名单重建帧：仅保留标准字段，
// 剔除上游噪声（finish_reason:"" → null、空 content/refusal、空 tool_calls 列表、
// 空占位 function_call、顶层未知字段），空 delta 键一律省略，
// usage 缺失 → null，保证任意标准客户端按规范解析。
func normalizeFrame(obj map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range []string{"id", "object", "created", "model", "system_fingerprint", "service_tier"} {
		if v, ok := obj[k]; ok && v != nil {
			out[k] = v
		}
	}
	if _, ok := out["object"]; !ok {
		out["object"] = "chat.completion.chunk"
	}
	if _, ok := out["id"]; !ok {
		out["id"] = "chatcmpl-wb2api"
	}
	if chs, ok := obj["choices"].([]any); ok {
		nchs := make([]any, 0, len(chs))
		for _, ci := range chs {
			c, ok := ci.(map[string]any)
			if !ok {
				continue
			}
			nc := map[string]any{}
			if idx, ok := c["index"]; ok {
				nc["index"] = idx
			}
			delta := map[string]any{}
			if d, ok := c["delta"].(map[string]any); ok {
				if v, ok := d["role"].(string); ok && v != "" {
					delta["role"] = v
				}
				if v, ok := d["content"].(string); ok && v != "" {
					delta["content"] = v
				}
				if v, ok := d["reasoning_content"].(string); ok && v != "" {
					delta["reasoning_content"] = v
				}
				if v, ok := d["refusal"].(string); ok && v != "" {
					delta["refusal"] = v
				}
				if tcs, ok := d["tool_calls"].([]any); ok && len(tcs) > 0 {
					delta["tool_calls"] = tcs
				}
				if fc, ok := d["function_call"]; ok && fc != nil {
					// 空占位 function_call（name/arguments 全空）视为噪声剔除
					keep := false
					if fcm, ok2 := fc.(map[string]any); ok2 {
						n, _ := fcm["name"].(string)
						a, _ := fcm["arguments"].(string)
						keep = n != "" || a != ""
					} else {
						keep = true
					}
					if keep {
						delta["function_call"] = fc
					}
				}
			}
			nc["delta"] = delta
			if fr, ok := c["finish_reason"].(string); ok && fr != "" {
				nc["finish_reason"] = fr
			} else {
				nc["finish_reason"] = nil
			}
			nchs = append(nchs, nc)
		}
		out["choices"] = nchs
	}
	if rawUsage, ok := obj["usage"]; ok {
		if u, ok := rawUsage.(map[string]any); ok {
			out["usage"] = normalizeUsageCacheAliases(u)
		} else {
			out["usage"] = rawUsage
		}
	} else {
		out["usage"] = nil
	}
	return out
}

// Stream 透传上游 SSE 到 w（逐帧规范化后 flush），保证至少写一个 [DONE]。
// 调用方必须先设置过 status 200；本函数自设 SSE headers。
// 流式策略：逐帧透传（规范化已剥空 content 噪声），恢复与上游一致的平滑流式。
//
// StreamHint 变体（gateway_hint 任务）：上游 error 帧透传时附加
// error.gateway_hint 字段——message 原文不动，hint 并列补充；hintFn 返回空串
// 或 nil 时与 Stream 行为逐字节一致。
func Stream(w http.ResponseWriter, r io.Reader) error {
	return StreamHint(w, r, nil)
}

// StreamHint 同 Stream，但上游 error 帧透出前把 hintFn(payload) 的返回值写入
// error.gateway_hint。hintFn 为 nil 或返回空串 → 原样透传（零改写）。
// 空流兜底 error 帧（"empty upstream stream"）不带 hint（网关本地故障形态
// 未覆盖，不编造）。
func StreamHint(w http.ResponseWriter, r io.Reader, hintFn func(string) string) error {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	fl, _ := w.(http.Flusher)

	// toolCallSeen 跨帧记录 delta.tool_calls 里已发过首片的 index，
	// 供逐 chunk 透传时收敛 name 为「每 index 一次」（对齐 OpenAI 官方流）。
	toolCallSeen := map[toolCallIdentity]bool{}
	firstID := ""
	sawDone := false
	choiceSeen := map[int]bool{}
	choiceFinished := map[int]bool{}
	allChoicesFinished := func() bool {
		if len(choiceSeen) == 0 || !choiceFinished[0] {
			return false
		}
		for idx := range choiceSeen {
			if !choiceFinished[idx] {
				return false
			}
		}
		return true
	}
	sawUpstreamError := false

	// writeRaw 原样写出一帧（绕过 normalizeFrame）并 flush。上游 error 帧（error-passthrough）
	// 与空流错误帧需保留 error 字段，不能被白名单剥掉，故经此写出。
	// gateway_hint：上游 error 帧透出前按 hintFn 附加 error.gateway_hint 字段
	// （error 对象上加一个键，message/code/requestId 等原文不动；hintFn 为
	// nil / 空串 / 非 JSON 帧 → 原样写出，零改写）。
	writeRaw := func(payload string) error {
		if strings.Contains(payload, "\n") {
			var compact bytes.Buffer
			if json.Compact(&compact, []byte(payload)) == nil {
				payload = compact.String()
			}
		}
		if hint := frameGatewayHint(hintFn, payload); hint != "" {
			payload = attachHintToErrorFrame(payload, hint)
		}
		if _, werr := io.WriteString(w, "data: "+payload+"\n\n"); werr != nil {
			return werr
		}
		if fl != nil {
			fl.Flush()
		}
		return nil
	}

	// writeErrorEvent 序列化生成标准合规的 SSE error 帧，防止手动字符串拼接引入引号/换行破坏 JSON 或 SSE 协议
	writeErrorEvent := func(code, msg string) error {
		payload, err := json.Marshal(map[string]any{
			"error": map[string]any{
				"message": msg,
				"type":    "upstream_error",
				"code":    code,
			},
		})
		if err != nil {
			return err
		}
		return writeRaw(string(payload))
	}

	// writeFrame 把 payload 按规范白名单重建后以 data: 帧写出并 flush。
	// 仅 JSON 解析成功时计数记为一次有效转发（JSON 解析失败丢弃残缺行，不输出到客户端避免破坏客户端解析器）。
	writeFrame := func(payload string) (int, error) {
		var obj map[string]any
		valid := 0
		if json.Unmarshal([]byte(payload), &obj) == nil && obj != nil && hasMeaningfulChunk(obj, true) {
			// 上游错误帧透传（error-passthrough）：带 error 键的帧**原样写出**，不走
			// normalizeFrame 白名单——白名单会剥掉 error 字段，客户端就看不到上游
			// code/msg/requestId。error.message 即上游原文（如 6004 限流、审核拦截），
			// 计入有效帧（避免误判空流补写 "empty upstream stream"）。
			if hasUpstreamError(obj) {
				sawUpstreamError = true
				if werr := writeRaw(payload); werr != nil {
					return 0, werr
				}
				return 1, nil
			}
			if choices, ok := obj["choices"].([]any); ok {
				for position, item := range choices {
					choice, _ := item.(map[string]any)
					if choice == nil {
						continue
					}
					idx := position
					if n, ok := choice["index"].(float64); ok {
						idx = int(n)
					}
					choiceSeen[idx] = true
					if reason, ok := choice["finish_reason"].(string); ok && trustedFinishReason(reason) {
						choiceFinished[idx] = true
					}
				}
			}
			// 先按 index 收敛 tool_calls name（每 index 仅首片保留，后续分片删 name 键），再规范化透传。
			stripToolCallNames(obj, toolCallSeen)
			// id 续传：首帧非空真实 id 缓存；后续帧缺 id / 空 id 一律用缓存值，
			// 有自己 id 的帧保持原样（不同流分裂的帧允许各自 id）。
			if firstID == "" {
				if v, ok := obj["id"].(string); ok && v != "" {
					firstID = v
				}
			} else {
				if v, ok := obj["id"].(string); !ok || v == "" {
					obj["id"] = firstID
				}
			}
			if raw, err := json.Marshal(normalizeFrame(obj)); err == nil {
				payload = string(raw)
			}
			valid = 1
			if _, werr := io.WriteString(w, "data: "+payload+"\n\n"); werr != nil {
				return 0, werr
			}
			if fl != nil {
				fl.Flush()
			}
			return valid, nil
		}
		// 非合法 JSON 帧（如网络断开导致的半行残缺文本）：丢弃，不输出给客户端
		return 0, nil
	}

	br := newSSEEventReader(r)
	validFrames := 0
readLoop:
	for {
		payload, complete, err := br.readDataEvent()
		if err == io.EOF {
			break
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return fmt.Errorf("%w: %w", errStreamTruncated, err)
			}
			if sawUpstreamError {
				break
			}
			if allChoicesFinished() {
				sawDone = true
				break
			}
			if validFrames == 0 || len(choiceSeen) == 0 {
				if werr := writeErrorEvent("upstream_parse", "upstream connection closed before emitting frames: "+err.Error()); werr != nil {
					return werr
				}
				if werr := writeDone(w, fl); werr != nil {
					return werr
				}
				return fmt.Errorf("%w: %w", errEmptyStream, err)
			}
			if werr := writeErrorEvent("stream_error", "upstream stream reading error: "+err.Error()); werr != nil {
				return werr
			}
			if werr := writeDone(w, fl); werr != nil {
				return werr
			}
			return fmt.Errorf("%w: %w", errStreamTruncated, err)
		}
		if !complete {
			continue
		}
		switch {
		case payload == "[DONE]":
			// 上游显式结束：停止读取，DONE 之后的任何数据（含垃圾帧）一律不再透传。
			// [DONE] 统一在循环结束后写出，保证恰好一个。
			sawDone = true
			break readLoop
		default:
			n, werr := writeFrame(payload)
			validFrames += n
			if werr != nil {
				return werr
			}
		}
	}
	// 空流（0 有效帧）：写一帧 error，再补 [DONE]，并返回 errEmptyStream 供调用方记录
	if !sawUpstreamError && (validFrames == 0 || len(choiceSeen) == 0) {
		if err := writeErrorEvent("upstream_parse", "empty upstream stream"); err != nil {
			return err
		}
		if err := writeDone(w, fl); err != nil {
			return err
		}
		return errEmptyStream
	}
	// Any upstream error dominates previous finish metadata and DONE markers.
	if sawUpstreamError {
		if err := writeDone(w, fl); err != nil {
			return err
		}
		return errUpstreamInStreamError
	}
	// 上游流中途非正常截断（有有效帧，但未见 finish_reason、未见 [DONE]、也无 error 帧就 EOF）：
	// 写入显式错误帧并补 [DONE]，返回 errStreamTruncated 供 handler 观测与处置，
	// 避免向客户端伪造“正常生成完毕”而造成静默腰斩。
	if !sawDone && !allChoicesFinished() {
		if err := writeErrorEvent("stream_truncated", "upstream stream truncated prematurely"); err != nil {
			return err
		}
		if err := writeDone(w, fl); err != nil {
			return err
		}
		return errStreamTruncated
	}
	// 保证恰好写一个 [DONE]（正常流收尾，或上游漏发时兜底补上）。
	return writeDone(w, fl)
}

func writeDone(w io.Writer, fl http.Flusher) error {
	if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
		return err
	}
	if fl != nil {
		fl.Flush()
	}
	return nil
}

// frameGatewayHint 取 error 帧的 gateway_hint（hintFn 缺失/异常返回空串 → 不附加）。
func frameGatewayHint(hintFn func(string) string, payload string) string {
	if hintFn == nil {
		return ""
	}
	// panic 隔离：hint 判定是补充功能，任何实现缺陷不得击穿流透传主路径。
	defer func() { _ = recover() }()
	return strings.TrimSpace(hintFn(payload))
}

// attachHintToErrorFrame 在 error 帧的 error 对象上附加 gateway_hint 字段。
// message/code/requestId 等既有键原样保留（只加不改）；非 JSON / 无 error 对象 →
// payload 原样返回（宁可不加 hint 也不破坏原文透传）。
func attachHintToErrorFrame(payload, hint string) string {
	var obj map[string]any
	if json.Unmarshal([]byte(payload), &obj) != nil {
		return payload
	}
	e, ok := obj["error"].(map[string]any)
	if !ok {
		return payload
	}
	e["gateway_hint"] = hint
	out, err := json.Marshal(obj)
	if err != nil {
		return payload
	}
	return string(out)
}
