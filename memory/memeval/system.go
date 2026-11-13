package memeval

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/memory"
)

// System 是被评测的记忆方案。Ingest 灌入一个 Case 的历史，返回只服务该 Case 的检索器；不同 Case 可并发灌入。
type System interface {
	Name() string
	Ingest(ctx context.Context, c Case) (Retriever, error)
}

// Retriever 为一道题给出放入请求的上下文。
type Retriever interface {
	Context(ctx context.Context, q Question) (Retrieved, error)
}

// Retrieved 是一道题取到的上下文；Sessions 是上下文内容所依据的会话 ID，用于计算证据召回率。
type Retrieved struct {
	Text     string
	Sessions []string
}

// Recent 是没有长期记忆的基线：从最近的会话往前取原文，直到 Runes 个字符，相当于只靠会话日志与上下文窗口。
type Recent struct{ Runes int }

func (Recent) Name() string { return "recent" }

func (r Recent) Ingest(_ context.Context, c Case) (Retriever, error) {
	return recentCase{c: c, runes: cmp.Or(r.Runes, constants.DefaultMemoryRecallRunes)}, nil
}

type recentCase struct {
	c     Case
	runes int
}

func (r recentCase) Context(_ context.Context, q Question) (Retrieved, error) {
	budget := r.runes
	var blocks []string
	var ids []string
	for i := len(r.c.Sessions) - 1; i >= 0 && budget > 0; i-- {
		s := r.c.Sessions[i]
		if s.Date.After(q.Date) && !q.Date.IsZero() {
			continue
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "[%s]\n", s.Date.Format(time.DateTime))
		for _, t := range s.Turns {
			fmt.Fprintf(&sb, "%s: %s\n", t.Role, t.Content)
		}
		text := []rune(sb.String())
		if len(text) > budget {
			// 保留会话末尾：越靠近提问时间越可能相关。
			text = text[len(text)-budget:]
		}
		budget -= len(text) + 1 // 会话之间的换行
		blocks = append(blocks, string(text))
		ids = append(ids, s.ID)
	}
	slices.Reverse(blocks)
	return Retrieved{Text: strings.Join(blocks, "\n"), Sessions: ids}, nil
}

// Memory 是长期记忆方案：每个会话作为一轮写入会话日志并登记抽取任务，按时间顺序处理完后按题召回。
// Configure 在每个 Case 的新存储上补齐配置（模型、嵌入、是否启用图谱与社区等）；没有给出 Durable 时，
// 每个 Case 在 Dir（空时为系统临时目录）下新建一个 FileStore，由调用方在评测结束后删除 Dir。
type Memory struct {
	Label     string
	Configure func(sessions session.Store) memory.Config
	Dir       string
}

func (m *Memory) Name() string { return m.Label }

func (m *Memory) Ingest(ctx context.Context, c Case) (Retriever, error) {
	sessions := session.NewMemoryStore()
	cfg := m.Configure(sessions)
	if cfg.Durable == nil {
		dir, err := os.MkdirTemp(m.Dir, "memeval-")
		if err != nil {
			return nil, err
		}
		if cfg.Durable, err = durable.NewFileStore(dir); err != nil {
			return nil, err
		}
	}
	mem, err := memory.New(cfg)
	if err != nil {
		return nil, err
	}
	ns := []string{c.ID}
	for _, s := range c.Sessions {
		from, to, err := appendSession(ctx, sessions, s)
		if err != nil {
			return nil, err
		}
		turn, err := mem.Turn(memory.WithNamespace(ctx, ns...))
		if err != nil {
			return nil, err
		}
		if err := mem.Enqueue(ctx, memory.Job{Key: s.ID, Namespace: ns, Session: s.ID, From: from, To: to, Scope: turn.Scope, TurnSeq: turn.Seq}); err != nil {
			return nil, err
		}
		// 按时间顺序逐个处理，使后面的会话能更新或否定前面的记忆；失败的任务退避后按 JobAttempts 重试，直到完成或标记失败。
		var j memory.Job
		for j.Status != memory.JobDone && j.Status != memory.JobFailed {
			if j.Attempts > 0 {
				if err := sleep(ctx, chatBackoff(j.Attempts)); err != nil {
					return nil, err
				}
			}
			if _, err := mem.ProcessPending(ctx); err != nil {
				return nil, err
			}
			if j, _, err = mem.Job(memory.WithNamespace(ctx, ns...), s.ID); err != nil {
				return nil, err
			}
		}
		if j.Status != memory.JobDone {
			return nil, agentErrors.Newf(agentErrors.CodeLLMResponse, "会话 %s 抽取失败：%s", s.ID, j.Error).WithComponent("memeval").WithOperation("ingest")
		}
	}
	return memoryCase{mem: mem, ns: ns}, nil
}

type memoryCase struct {
	mem *memory.Memory
	ns  []string
}

func (m memoryCase) Context(ctx context.Context, q Question) (Retrieved, error) {
	got, err := m.mem.Retrieve(memory.WithNamespace(ctx, m.ns...), q.Text)
	if err != nil {
		return Retrieved{}, err
	}
	return Retrieved{Text: got.Text, Sessions: got.Sessions()}, nil
}

// appendSession 把一个会话作为一轮写入会话日志，轮次开始事件的时间取会话日期，使记忆的生效时间与对话发生时间一致。
func appendSession(ctx context.Context, sessions session.Store, s Session) (int64, int64, error) {
	if len(s.Turns) == 0 {
		return 0, 0, invalid("会话 %s 没有消息", s.ID)
	}
	rec, _, err := session.OpenRecorder(ctx, sessions, s.ID)
	if err != nil {
		return 0, 0, err
	}
	start, err := session.NewEvent(constants.SessionEventTurnStart, session.TurnStart{Input: s.Turns[0].Content})
	if err != nil {
		return 0, 0, err
	}
	start.Time = s.Date
	if _, err := rec.Append(ctx, start); err != nil {
		return 0, 0, err
	}
	from := rec.Seq()
	derived, err := session.DeriveMessages(rec.Events())
	if err != nil {
		return 0, 0, err
	}
	if err := rec.Sync(ctx, append(derived, s.Turns...), nil); err != nil {
		return 0, 0, err
	}
	end, err := session.NewEvent(constants.SessionEventTurnEnd, session.TurnEnd{})
	if err != nil {
		return 0, 0, err
	}
	if _, err := rec.Append(ctx, end); err != nil {
		return 0, 0, err
	}
	return from, rec.Seq(), nil
}
