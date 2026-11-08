package constants

// 会话日志事件类型；对应数据结构仍由 core/session 定义。
const (
	// SessionEventTurnStart 开始一轮，数据为 TurnStart。
	SessionEventTurnStart = "turn/start"
	// SessionEventTurnEnd 结束一轮，数据为 TurnEnd；中止轮次的 Surface 撤销该轮全部事件。
	SessionEventTurnEnd = "turn/end"
	// SessionEventSystemMessage 记录变化后的系统提示词，数据为 MessageData；只供导出。
	SessionEventSystemMessage = "system/message"
	// SessionEventToolDefinitions 记录变化后的工具定义，数据为 ToolsData；只供导出。
	SessionEventToolDefinitions = "tool/definitions"
	// 消息事件的数据为 MessageData；工具结果另带工具元数据。
	SessionEventUserMessage      = "user/message"
	SessionEventAssistantMessage = "assistant/message"
	SessionEventToolResult       = "tool/result"
	// SessionEventHistoryRewrite 的 RewriteData 替换 Surface 指定区间中的消息。
	SessionEventHistoryRewrite = "history/rewrite"
	// SessionEventRewind 撤销 Surface 指定区间中的全部事件。
	SessionEventRewind = "session/rewind"
	// SessionEventBoundarySnapshot 的 BoundaryData 保存一次 durable 操作快照。
	SessionEventBoundarySnapshot = "boundary/snapshot"
)
