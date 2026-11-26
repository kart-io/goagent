package builder

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"strconv"
	"strings"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
)

const (
	modelCacheKeyPrefix = "mc1:"
	cachedTurnMagic     = "mc1\n"
)

// cacheKeyInput 是缓存键的全部输入，任一字段变化都得到不同的键。
type cacheKeyInput struct {
	Identity  string
	SessionID string
	Client    string
	Request   *llm.CompletionRequest
	Tools     []interfaces.Tool
	Memory    []memory.NamespaceVersion
	// Images 是图片解析配置的身份，未配置解析器时为空。
	Images string
}

// lengthWriter 把每个字段先写 8 字节大端长度再写内容，拼接边界不会产生歧义。
type lengthWriter struct{ h hash.Hash }

func (w lengthWriter) bytes(b []byte) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(b)))
	w.h.Write(n[:])
	w.h.Write(b)
}

func (w lengthWriter) str(s string) { w.bytes([]byte(s)) }

func (w lengthWriter) num(n int64) { w.str(strconv.FormatInt(n, 10)) }

func (w lengthWriter) strs(ss []string) {
	w.num(int64(len(ss)))
	for _, s := range ss {
		w.str(s)
	}
}

// modelCacheKey 计算缓存键；工具 schema 不是合法 JSON 或请求无法序列化时 ok=false。
func modelCacheKey(in cacheKeyInput) (string, bool) {
	reqJSON, err := json.Marshal(in.Request)
	if err != nil {
		return "", false
	}
	h := sha256.New()
	w := lengthWriter{h}
	w.str(in.Identity)
	w.str(in.SessionID)
	w.str(in.Client)
	w.bytes(reqJSON)
	w.num(int64(len(in.Tools)))
	for _, t := range in.Tools {
		schema, ok := canonicalSchema(t.ArgsSchema())
		if !ok {
			return "", false
		}
		w.str(t.Name())
		w.str(t.Description())
		w.bytes(schema)
	}
	w.num(int64(len(in.Memory)))
	for _, v := range in.Memory {
		w.strs(v.Namespace)
		w.str(v.Scope)
		w.num(v.LastForgetSeq)
		w.num(v.LastRewindSeq)
	}
	// 只在配置了图片解析器时追加，已有缓存键保持不变。
	if in.Images != "" {
		w.str(in.Images)
	}
	return modelCacheKeyPrefix + hex.EncodeToString(h.Sum(nil)), true
}

// canonicalSchema 把 schema 解析后重新序列化，使键顺序不同的等价 schema 得到相同结果；空 schema 视为无参数。
// 数字按原文保留，避免超过 2^53 的整数经 float64 合并成同一个键。
func canonicalSchema(schema string) ([]byte, bool) {
	if strings.TrimSpace(schema) == "" {
		return nil, true
	}
	var v any
	if err := json.Unmarshal([]byte(schema), &v, json.UseNumber); err != nil {
		return nil, false
	}
	out, err := json.Marshal(v)
	return out, err == nil
}

// clientIdentity 返回区分模型部署的身份：客户端声明了 CacheIdentity 时用它，否则用提供商加解析后的模型名；
// 两者都取不到时 ok=false。
func clientIdentity(c llm.Client, req *llm.CompletionRequest) (string, bool) {
	if ci, ok := c.(interface{ CacheIdentity() string }); ok {
		if id := ci.CacheIdentity(); id != "" {
			return id, true
		}
	}
	model := req.Model
	if model == "" {
		if named, ok := c.(interface{ ModelName() string }); ok {
			model = named.ModelName()
		}
	}
	if model == "" {
		return "", false
	}
	return string(c.Provider()) + "\x00" + model, true
}

// tagDigest 以 HMAC 摘要标签，不在后端暴露明文的会话 ID 或命名空间。
func tagDigest(tagKey []byte, kind string, write func(lengthWriter)) string {
	h := hmac.New(sha256.New, tagKey)
	w := lengthWriter{h}
	w.str(kind)
	write(w)
	return hex.EncodeToString(h.Sum(nil))
}

func sessionTag(tagKey []byte, sessionID string) string {
	return tagDigest(tagKey, "session", func(w lengthWriter) { w.str(sessionID) })
}

func memoryTag(tagKey []byte, ns []string, scope string) string {
	return tagDigest(tagKey, "memory", func(w lengthWriter) { w.strs(ns); w.str(scope) })
}

func encodeCachedTurn(t *llm.StreamChunk) ([]byte, error) {
	body, err := json.Marshal(t)
	if err != nil {
		return nil, fmt.Errorf("编码缓存的模型回合: %w", err)
	}
	return append([]byte(cachedTurnMagic), body...), nil
}

func decodeCachedTurn(b []byte) (*llm.StreamChunk, error) {
	body, ok := strings.CutPrefix(string(b), cachedTurnMagic)
	if !ok {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "缓存的模型回合缺少 %q 前缀", cachedTurnMagic).WithComponent("builder").WithOperation("decode_cached_turn")
	}
	var t llm.StreamChunk
	if err := json.Unmarshal([]byte(body), &t); err != nil {
		return nil, fmt.Errorf("解码缓存的模型回合: %w", err)
	}
	return &t, nil
}

// identity 返回本次调用的隔离身份：Scope 优先；未配置 Scope 且有长期记忆时取绑定的命名空间。
func (mc ModelCache) identity(ctx context.Context, ltm bool) (string, bool) {
	if mc.Scope != nil {
		if id, ok := mc.Scope(ctx); ok && id != "" {
			return id, true
		}
		return "", false
	}
	if !ltm {
		return "", false
	}
	ns := memory.NamespaceFrom(ctx)
	if len(ns) == 0 {
		return "", false
	}
	var b strings.Builder
	for _, c := range ns {
		b.WriteString(strconv.Itoa(len(c)))
		b.WriteByte(':')
		b.WriteString(c)
	}
	return b.String(), true
}
