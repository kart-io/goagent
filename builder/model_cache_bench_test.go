package builder

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/memory"
)

const benchMemoryRecords = 1000

// 取自真实对话风格的中文片段，与 UUID 拼接，避免重复文本使压缩与缓存偏乐观。
var benchPhrases = []string{
	"用户上周在杭州出差，住在西湖边的酒店，对早餐的豆浆不满意",
	"订单退款流程需要财务二次审核，金额超过五千元时必须有主管签字",
	"客户偏好周二下午的电话回访，不希望在周末收到任何营销短信",
	"他养了两只英短猫，名字分别叫汤圆和年糕，每月在宠物医院做一次体检",
	"上季度的采购合同约定了三十天账期，供应商提出延长到四十五天",
	"她正在备考雅思，口语目标七分，每周和语伴练习两次",
}

type benchBackend struct {
	name string
	open func(b *testing.B) durable.Store
}

// pgstore 实现在 goagent-contrib 中，本模块无法构造；没有 DSN 时跳过。
var benchBackends = []benchBackend{
	{"FileStore", func(b *testing.B) durable.Store {
		fs, err := durable.NewFileStore(b.TempDir())
		require.NoError(b, err)
		return fs
	}},
	{"pgstore", func(b *testing.B) durable.Store {
		if os.Getenv("GOAGENT_PGSTORE_DSN") == "" {
			b.Skip("未设置 GOAGENT_PGSTORE_DSN；pgstore 组需在 goagent-contrib 环境中另行运行")
		}
		b.Skip("pgstore 实现位于 goagent-contrib，本模块无法构造")
		return nil
	}},
}

func benchMemory(b *testing.B, ctx context.Context, store durable.Store) *memory.Memory {
	b.Helper()
	m, err := memory.New(memory.Config{Durable: store, EraseWithin: 2 * time.Hour})
	require.NoError(b, err)
	for i := range benchMemoryRecords {
		content := fmt.Sprintf("%s（编号 %s，第 %d 条）", benchPhrases[i%len(benchPhrases)], uuid.NewString(), i)
		_, err := m.Remember(ctx, memory.Record{ID: uuid.NewString(), Content: content, Importance: 0.1 + float64(i%9)/10})
		require.NoError(b, err)
	}
	return m
}

// BenchmarkModelCacheVersion 度量 CacheVersion 本身，以及一次命中路径的总耗时（带与不带长期记忆）。
func BenchmarkModelCacheVersion(b *testing.B) {
	for _, be := range benchBackends {
		b.Run(be.name, func(b *testing.B) {
			store := be.open(b)
			ctx := memory.WithNamespace(context.Background(), "t1", "alice")
			m := benchMemory(b, ctx, store)
			mc := ModelCache{Cache: newCountingCache(), TTL: time.Hour, TagKey: testTagKey}

			b.Run("CacheVersion", func(b *testing.B) {
				read := [][]string{{"t1", "alice"}}
				b.ReportAllocs()
				for b.Loop() {
					if _, err := m.CacheVersion(ctx, read); err != nil {
						b.Fatal(err)
					}
				}
			})

			hit := func(b *testing.B, withMemory bool) {
				build := func(model *cacheModel) *ConfigurableAgent[*core.AgentState] {
					bld := NewSimpleBuilder(model).WithSessionID("s").WithSessionLog(session.NewMemoryStore()).WithTools(plainTool())
					if withMemory {
						bld = bld.WithLongTermMemory(m).WithModelCache(mc)
					} else {
						bld = bld.WithModelCache(ModelCache{Cache: mc.Cache, TTL: mc.TTL, Scope: scopeOf("u1"), TagKey: mc.TagKey})
					}
					agent, err := bld.Build()
					require.NoError(b, err)
					return agent
				}
				_, err := build(newCacheModel("dep", textTurn("答案"))).AsRunnable().Invoke(ctx, "问题")
				require.NoError(b, err)
				b.ReportAllocs()
				for b.Loop() {
					b.StopTimer()
					agent := build(newCacheModel("dep"))
					b.StartTimer()
					out, err := agent.AsRunnable().Invoke(ctx, "问题")
					if err != nil || out.Result != "答案" {
						b.Fatalf("未命中: %v", err)
					}
				}
			}
			b.Run("命中路径/带长期记忆", func(b *testing.B) { hit(b, true) })
			b.Run("命中路径/无长期记忆", func(b *testing.B) { hit(b, false) })
		})
	}
}
