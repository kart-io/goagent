package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kart-io/goagent/llm"
)

// BenchmarkSessionHistory 区分读取、无变化同步、追加和重写；变更场景在计时外重建固定长度历史。
// 文件初始化与清理不计时，使用固定迭代次数避免夹具重建占据过长墙钟时间。
func BenchmarkSessionHistory(b *testing.B) {
	for _, backend := range []string{"内存", "文件"} {
		for _, size := range []int{100, 1000, 10000} {
			for _, operation := range []string{"读取", "不变同步", "追加同步", "重写同步"} {
				b.Run(fmt.Sprintf("%s/%d/%s", backend, size, operation), func(b *testing.B) {
					ctx := context.Background()
					path := filepath.Join(b.TempDir(), "fixture")
					messages := make([]llm.Message, size)
					for i := range messages {
						messages[i] = llm.UserMessage(fmt.Sprintf("消息%d:%s", i, strings.Repeat("证据", 32)))
					}
					changed := append([]llm.Message(nil), messages...)
					if operation == "追加同步" {
						changed = append(changed, llm.AssistantMessage("最终回执"))
					}
					if operation == "重写同步" {
						changed[size/2].Content = "替换的历史摘要"
					}
					setup := func() (Store, *Recorder) {
						var store Store
						if backend == "文件" {
							if err := os.RemoveAll(path); err != nil {
								b.Fatal(err)
							}
							s, err := NewFileStore(path)
							if err != nil {
								b.Fatal(err)
							}
							store = s
						} else {
							store = NewMemoryStore()
						}
						r, _, err := OpenRecorder(ctx, store, "session")
						if err != nil {
							b.Fatal(err)
						}
						if err = r.Sync(ctx, messages, nil); err != nil {
							b.Fatal(err)
						}
						return store, r
					}
					store, r := setup()
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if operation == "追加同步" || operation == "重写同步" {
							b.StopTimer()
							store, r = setup()
							b.StartTimer()
						}
						if operation == "读取" {
							opened, events, err := OpenRecorder(ctx, store, "session")
							if err != nil || opened.Seq() != int64(size) || len(events) != size {
								b.Fatal("历史读取不完整", err)
							}
						} else {
							if err := r.Sync(ctx, changed, nil); err != nil {
								b.Fatal(err)
							}
						}
					}
					b.StopTimer()
					derived, err := DeriveMessages(r.Events())
					if err != nil {
						b.Fatal(err)
					}
					if operation != "读取" && len(derived) != len(changed) {
						b.Fatal("同步结果长度错误")
					}
					if operation == "重写同步" && derived[size/2].Content != changed[size/2].Content {
						b.Fatal("历史改写未生效")
					}
				})
			}
		}
	}
}
