package performance

import (
	"bytes"
	"sync"
	"sync/atomic"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
)

// ObjectPoolStats 对象池统计信息（内部使用，包含原子操作）
type ObjectPoolStats struct {
	Gets    atomic.Int64 // 获取次数
	Puts    atomic.Int64 // 归还次数
	News    atomic.Int64 // 新建次数
	Current atomic.Int64 // 当前池中对象数（估算）
}

// ObjectPoolStatsSnapshot 对象池统计信息快照（用于返回和显示）
type ObjectPoolStatsSnapshot struct {
	Gets    int64 // 获取次数
	Puts    int64 // 归还次数
	News    int64 // 新建次数
	Current int64 // 当前池中对象数（估算）
}

// AllPoolStats 所有对象池的统计信息
type AllPoolStats struct {
	ByteBuffer  ObjectPoolStats
	Message     ObjectPoolStats
	ToolInput   ObjectPoolStats
	ToolOutput  ObjectPoolStats
	AgentInput  ObjectPoolStats
	AgentOutput ObjectPoolStats
}

// ByteBufferPool bytes.Buffer 对象池
//
// 用于高频场景的 Buffer 复用，减少内存分配和 GC 压力
// 常见使用场景：JSON 序列化、HTTP 请求体构建、字符串拼接
var ByteBufferPool = &sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

// MessagePool interfaces.Message 对象池
//
// 用于 LLM 通信中的消息对象复用
var MessagePool = &sync.Pool{
	New: func() interface{} {
		return &interfaces.Message{}
	},
}

// ToolInputPool interfaces.ToolInput 对象池
//
// 用于工具调用输入对象的复用
var ToolInputPool = &sync.Pool{
	New: func() interface{} {
		return &interfaces.ToolInput{
			Args: make(map[string]interface{}),
		}
	},
}

// ToolOutputPool interfaces.ToolOutput 对象池
//
// 用于工具调用输出对象的复用
var ToolOutputPool = &sync.Pool{
	New: func() interface{} {
		return &interfaces.ToolOutput{
			Metadata: make(map[string]interface{}),
		}
	},
}

// AgentInputPool core.AgentInput 对象池
//
// 用于 Agent 输入对象的复用
var AgentInputPool = &sync.Pool{
	New: func() interface{} {
		return &core.AgentInput{
			Context: make(map[string]interface{}),
		}
	},
}

// AgentOutputPool core.AgentOutput 对象池
//
// 用于 Agent 输出对象的复用
var AgentOutputPool = &sync.Pool{
	New: func() interface{} {
		return &core.AgentOutput{
			ReasoningSteps: make([]core.ReasoningStep, 0, 4),
			ToolCalls:      make([]core.ToolCall, 0, 4),
			Metadata:       make(map[string]interface{}),
		}
	},
}
