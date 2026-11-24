package constants

// Session log event types; their payload types are defined in core/session.
const (
	// SessionEventTurnStart starts a turn; payload TurnStart.
	SessionEventTurnStart = "turn/start"
	// SessionEventTurnEnd ends a turn; payload TurnEnd. The Surface of an
	// aborted turn reverts all of its events.
	SessionEventTurnEnd = "turn/end"
	// SessionEventSystemMessage records a changed system prompt; payload
	// MessageData, export only.
	SessionEventSystemMessage = "system/message"
	// SessionEventToolDefinitions records changed tool definitions; payload
	// ToolsData, export only.
	SessionEventToolDefinitions = "tool/definitions"
	// Message events carry MessageData; tool results also carry tool metadata.
	SessionEventUserMessage      = "user/message"
	SessionEventAssistantMessage = "assistant/message"
	SessionEventToolResult       = "tool/result"
	// SessionEventHistoryRewrite replaces the messages in the Surface range with
	// its RewriteData.
	SessionEventHistoryRewrite = "history/rewrite"
	// SessionEventRewind reverts every event in the Surface range.
	SessionEventRewind = "session/rewind"
	// SessionEventBoundarySnapshot stores a durable operation snapshot in its
	// BoundaryData.
	SessionEventBoundarySnapshot = "boundary/snapshot"
)
