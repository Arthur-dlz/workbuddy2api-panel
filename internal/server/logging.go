// logging.go 请求级表格日志：每个 /v1/chat/completions 请求结束后打印一行到 stdout。
package server

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/logfmt"
	"github.com/linguo2625469/workbuddy2api-panel/internal/pool"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// chatSeq 进程级请求序号。
var chatSeq atomic.Int64

// chatLogEnabled 聊天表格日志总开关。生产恒 true；
// 测试包经 TestMain 置 false 关闭 stdout 噪音，需要断言行输出的测试用 withChatLog 临时开启（R5）。
var chatLogEnabled = true

// chatLogOut 聊天表格日志的输出目标。生产默认 os.Stdout；main 在启用管理面板时
// 经 SetChatLogOutput 注入 MultiWriter，把每行镜像进 /panel/api/logs 的环形缓冲，
// stdout 行为不变。需在开始服务前调用一次（无并发竞争窗口）。
var chatLogOut io.Writer = os.Stdout

// SetChatLogOutput 替换聊天表格日志输出目标（仅 main 启动期调用一次）。
func SetChatLogOutput(w io.Writer) { chatLogOut = w }

// chatStat 单个 chat 请求的日志统计；handler 挂 defer，请求出口后落一行。
type chatStat struct {
	start               time.Time
	model               string
	mode                string // "stream" | "sync"
	uid                 string // 完整 uid，展示时只取前 8 位
	nick                string // 账号昵称（随选号同步），流水行经 logfmt.Label 拼成 "昵称(uid8)"
	ttfb                time.Duration
	toks                int // <0 表示 usage 缺失 → 显示 "-"
	status              int
	clientCancelled     bool
	responseCommitted   bool
	responseWriteFailed bool

	logged bool
}

// newChatStat 以请求进入 handler 的时刻为起点构造统计对象；toks 默认 -1（usage 缺失）。
func newChatStat(now time.Time, body []byte, stream bool) *chatStat {
	mode := "sync"
	if stream {
		mode = "stream"
	}
	return &chatStat{start: now, model: parseModelFromBody(body), mode: mode, toks: -1}
}

// done 幂等落一行表格日志。
func (s *chatStat) done() {
	if s.logged {
		return
	}
	s.logged = true
	logChatRow(s.ttfb, time.Since(s.start), s.model, s.mode, s.uid, s.nick, s.status, s.toks)
	if s.clientCancelled || !s.responseCommitted || s.responseWriteFailed {
		fmt.Fprintf(chatLogOut, "[server] client_cancelled=%t response_committed=%t response_write_failed=%t\n", s.clientCancelled, s.responseCommitted, s.responseWriteFailed)
	}
}

type trackedResponseWriter struct {
	http.ResponseWriter
	status      int
	committed   bool
	writeFailed bool
}

func (w *trackedResponseWriter) WriteHeader(status int) {
	if w.committed {
		return
	}
	w.status = status
	w.committed = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *trackedResponseWriter) Write(p []byte) (int, error) {
	if !w.committed {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(p)
	if err != nil || n != len(p) {
		w.writeFailed = true
	}
	return n, err
}

func (w *trackedResponseWriter) Flush() {
	if !w.committed {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *trackedResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// chatStatsReader 在流式透传时抓取 SSE 末帧的 usage.completion_tokens 精确值，
// 并记录首个 data 帧的 TTFB；原始字节原样返回给下游透传。
// 注意：不做 rune 估算，token 数一律采信上游 usage。
type chatStatsReader struct {
	src                 io.Reader
	start               time.Time
	ttfb                time.Duration
	seen                bool // 已见过首个 data 帧（TTFB 只记一次）
	promptTokens        int
	completionTokens    int
	totalTokens         int
	hasPromptTokens     bool
	hasCompletionTokens bool
	hasTotalTokens      bool
	// credit 上游末帧 usage.credit（本次真实扣费积分），供成本台账（NoteModelCost）。
	hasCredit bool
	credit    float64
	// cachedTokens 上游末帧 usage 中提取的 prompt cache 命中 token 数量。
	hasCachedTokens bool
	cachedTokens    int64
	pend            []byte // 已读未返回的行缓存
	line            strings.Builder
	data            []string
	skipLF          bool
}

// newChatStatsReaderSince 以 since 为 TTFB 计时起点（通常是请求进入 handler 的时刻）。
func newChatStatsReaderSince(r io.Reader, since time.Time) *chatStatsReader {
	return &chatStatsReader{src: r, start: since}
}

// TTFB 返回首个 data 帧到达耗时；无帧时为 0。
func (s *chatStatsReader) TTFB() time.Duration { return s.ttfb }

// Tokens 返回末帧 usage.completion_tokens 与是否缺失；无 usage 时 ok=false。
func (s *chatStatsReader) Tokens() (int, bool) { return s.completionTokens, s.hasCompletionTokens }

// Credit 返回末帧 usage.credit（本次真实扣费积分）与是否缺失。
func (s *chatStatsReader) Credit() (float64, bool) { return s.credit, s.hasCredit }

// TotalTokens 返回末帧 usage.total_tokens 与是否缺失。
func (s *chatStatsReader) TotalTokens() (int, bool) { return s.totalTokens, s.hasTotalTokens }

// CachedTokens 返回末帧 usage 中提取的 prompt cache 命中 token 数量与是否命中。
func (s *chatStatsReader) CachedTokens() (int64, bool) { return s.cachedTokens, s.hasCachedTokens }

// Usage 返回流式响应中已收到的 token usage 字段。
func (s *chatStatsReader) Usage() pool.TokenUsageDelta {
	return pool.TokenUsageDelta{
		HasPromptTokens:     s.hasPromptTokens,
		PromptTokens:        int64(s.promptTokens),
		HasCompletionTokens: s.hasCompletionTokens,
		CompletionTokens:    int64(s.completionTokens),
		HasTotalTokens:      s.hasTotalTokens,
		TotalTokens:         int64(s.totalTokens),
	}
}

// parseSSELine 解析一行 "data: {...}"：首帧记 TTFB，含 usage 时采信精确 completion_tokens 与 prompt 缓存。
func (s *chatStatsReader) parseSSELine(line string) {
	line = strings.TrimRight(line, "\r\n")
	if strings.HasPrefix(line, ":") {
		return
	}
	field, value, hasValue := strings.Cut(line, ":")
	if field != "data" {
		return
	}
	if hasValue {
		value = strings.TrimPrefix(value, " ")
	}
	s.parseSSEPayload(value)
}

func (s *chatStatsReader) parseSSEPayload(payload string) {
	if payload == "[DONE]" {
		return
	}
	if !s.seen {
		s.seen = true
		s.ttfb = time.Since(s.start)
	}

	// 性能剪枝：首帧记录 TTFB 后，后续 99% 的纯文本正文帧均不含 "usage"，直接短路退出，消除大量 json 反序列化开销
	if !strings.Contains(payload, `"usage"`) {
		return
	}

	var chunk struct {
		Usage map[string]any `json:"usage"`
	}
	if json.Unmarshal([]byte(payload), &chunk) != nil || chunk.Usage == nil {
		return
	}

	if pt, ok := readIntMetric(chunk.Usage["prompt_tokens"]); ok {
		s.hasPromptTokens = true
		s.promptTokens = pt
	}
	if ct, ok := readIntMetric(chunk.Usage["completion_tokens"]); ok {
		s.hasCompletionTokens = true
		s.completionTokens = ct
	}
	if tt, ok := readIntMetric(chunk.Usage["total_tokens"]); ok {
		s.hasTotalTokens = true
		s.totalTokens = tt
	}
	if cr, ok := readFloatMetric(chunk.Usage["credit"]); ok {
		s.hasCredit = true
		s.credit = cr
	}
	if ch, ok := upstream.BestUsageCacheHitTokens(chunk.Usage); ok && ch > 0 {
		s.hasCachedTokens = true
		s.cachedTokens = ch
	}
}

// isValidCredit 校验积分值，防御 NaN / Inf / 负数及异常畸形超大值（>=1e9）。
func isValidCredit(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v < 1e9
}

func readIntMetric(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 1e12 {
			return 0, false
		}
		return int(n), true
	case float32:
		f := float64(n)
		if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > 1e12 {
			return 0, false
		}
		return int(n), true
	case int:
		return n, n >= 0
	case int64:
		if n < 0 || n > 1e12 {
			return 0, false
		}
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil || i < 0 || i > 1e12 {
			return 0, false
		}
		return int(i), true
	}
	return 0, false
}

func readFloatMetric(v any) (float64, bool) {
	var val float64
	switch n := v.(type) {
	case float64:
		val = n
	case float32:
		val = float64(n)
	case int:
		val = float64(n)
	case int64:
		val = float64(n)
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0, false
		}
		val = f
	default:
		return 0, false
	}
	if isValidCredit(val) {
		return val, true
	}
	return 0, false
}

// Read 返回原始数据，同时解析统计 TTFB/token。
func (s *chatStatsReader) Read(p []byte) (int, error) {
	n, err := s.src.Read(p)
	if n > 0 {
		s.observeSSE(p[:n])
	}
	return n, err
}

// observeSSE observes event boundaries incrementally while Read returns every
// byte from the source immediately. CR, LF, CRLF, comments, and multiline data
// fields follow the same event framing used by the upstream SSE reader.
func (s *chatStatsReader) observeSSE(raw []byte) {
	for _, b := range raw {
		if s.skipLF {
			s.skipLF = false
			if b == '\n' {
				continue
			}
		}
		switch b {
		case '\r':
			s.finishObservedLine()
			s.skipLF = true
		case '\n':
			s.finishObservedLine()
		default:
			s.line.WriteByte(b)
		}
	}
}

func (s *chatStatsReader) finishObservedLine() {
	line := s.line.String()
	s.line.Reset()
	if line == "" {
		if len(s.data) != 0 {
			s.parseSSEPayload(strings.Join(s.data, "\n"))
			s.data = s.data[:0]
		}
		return
	}
	if strings.HasPrefix(line, ":") {
		return
	}
	field, value, hasValue := strings.Cut(line, ":")
	if field != "data" {
		return
	}
	if hasValue {
		value = strings.TrimPrefix(value, " ")
	}
	s.data = append(s.data, value)
}

// rewriteModel 把 outbound chat body 的 model 字段替换为 bare（保留其余字段原样）。
// 仅当 bare != 原 model 时由 chatCompletions 调用；body 不可解析时原样返回（不二次错误化）。
func rewriteModel(body []byte, bare string) []byte {
	if len(body) == 0 || bare == "" {
		return body
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return body
	}
	if cur, ok := obj["model"].(string); ok && cur == bare {
		return body
	}
	obj["model"] = bare
	out, err := json.Marshal(obj)
	if err != nil {
		return body
	}
	return out
}

// parseModelFromBody 从请求 JSON 取 model 字段，缺省标 "-"。
func parseModelFromBody(body []byte) string {
	var obj struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &obj); err != nil || obj.Model == "" {
		return "-"
	}
	return obj.Model
}

// usageDeltaFromResponse 从非流式聚合响应中提取明确存在的 token 字段。
func usageDeltaFromResponse(resp map[string]any) pool.TokenUsageDelta {
	delta := pool.TokenUsageDelta{}
	u, ok := resp["usage"].(map[string]any)
	if !ok || u == nil {
		return delta
	}
	read := func(key string) (int64, bool) {
		v, ok := u[key]
		if !ok || v == nil {
			return 0, false
		}
		switch n := v.(type) {
		case float64:
			if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 1e12 {
				return 0, false
			}
			return int64(n), true
		case float32:
			f := float64(n)
			if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > 1e12 {
				return 0, false
			}
			return int64(n), true
		case int:
			return int64(n), n >= 0
		case int64:
			return n, n >= 0
		case json.Number:
			i, err := n.Int64()
			return i, err == nil && i >= 0
		default:
			return 0, false
		}
	}
	if n, ok := read("prompt_tokens"); ok {
		delta.HasPromptTokens, delta.PromptTokens = true, n
	}
	if n, ok := read("completion_tokens"); ok {
		delta.HasCompletionTokens, delta.CompletionTokens = true, n
	}
	if n, ok := read("total_tokens"); ok {
		delta.HasTotalTokens, delta.TotalTokens = true, n
	}
	return delta
}

// completionTokens 从 Aggregate 返回的响应中提取 usage.completion_tokens；缺失返回 -1。
func completionTokens(resp map[string]any) int {
	u, ok := resp["usage"].(map[string]any)
	if !ok || u == nil {
		return -1
	}
	if v, ok := readIntMetric(u["completion_tokens"]); ok {
		return v
	}
	return -1
}

// uidPrefix 只显示 uid 前 8 位；空 uid 显示 "-"。
//
// 实现委托 logfmt.UID8，避免 "截 8 位" 的规则在 server 与 logfmt 两处各写一份而走样。
func uidPrefix(uid string) string {
	return logfmt.UID8(uid)
}

// 请求流水行的固定列宽（显示列宽，非字节）。取固定宽度而不是让内容自然长度撑开，
// 是为了让 stdout 里成百上千行能竖着扫——否则模型名长短不一、中文昵称按字节补空格
// 错位，根本没法用肉眼对齐着一列列看（这正是上一版 11 字节硬截断要解决的问题）。
const (
	// chatModelWidth 覆盖 realm 前缀 + 最长模型名："global:" (7) + "deepseek-v4.1-flash" (19) = 26。
	// 旧的 11 字节截断会把 "cn:deepseek-v4-flash" 切成 "cn:deepseek"，让人误以为是另一个模型。
	chatModelWidth = 26
	// chatAcctWidth 容纳 "昵称(uid8)"：中文昵称按 2 列/字算，5 字中文 + "(xxxxxxxx)" = 20 列。
	chatAcctWidth = 22
	chatTTFBWidth = 8
	chatTokWidth  = 6
	chatRateWidth = 11 // 形如 "183.6tok/s"
)

// logChatRow 打印一行请求级表格日志（输出 chatLogOut，无 log 时间戳前缀）。
//
// 参数：
//   - model：模型名（含 realm 前缀），超 chatModelWidth 截断（模型名是 ASCII，字节截即列宽）；
//   - uid/nick：完整 uid 与账号昵称，经 logfmt.Label 拼成 "昵称(uid8)" 展示——只有
//     uid8 时人眼无法判断是哪个号，要辨认必须再查 auths/，排障多一跳；
//   - toks<0 表示 usage 缺失，显示 "-"。
func logChatRow(ttfb, total time.Duration, model, mode, uid, nick string, status int, toks int) {
	if !chatLogEnabled {
		return
	}
	seq := chatSeq.Add(1)
	model = logfmt.Pad(logfmt.Truncate(model, chatModelWidth), chatModelWidth)
	// 账号标签只补不截：超宽时宁可让该行变宽，也不丢昵称信息（昵称是排查的主线索）。
	acct := logfmt.Pad(logfmt.Label(uid, nick), chatAcctWidth)
	tokField := "-"
	tokpsField := "-"
	if toks >= 0 {
		tokField = fmt.Sprintf("%d", toks)
		if total > 0 {
			tokpsField = fmt.Sprintf("%.1ftok/s", float64(toks)/total.Seconds())
		} else {
			tokpsField = "0.0tok/s"
		}
	}
	ttfbMS := "-"
	if ttfb > 0 {
		ttfbMS = fmt.Sprintf("%dms", ttfb.Milliseconds())
	}
	fmt.Fprintf(chatLogOut, "| #%03d | %s | %s | %s | %d | %s | TTFB=%s | tok=%s | %s | total=%.1fs |\n",
		seq,
		time.Now().Format("15:04:05"),
		model,
		mode,
		status,
		acct,
		logfmt.Pad(ttfbMS, chatTTFBWidth),
		logfmt.Pad(tokField, chatTokWidth),
		logfmt.Pad(tokpsField, chatRateWidth),
		total.Seconds(),
	)
}
