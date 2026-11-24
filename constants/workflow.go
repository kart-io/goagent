package constants

// Graph start, end and resume protocol values.
const (
	// WorkflowStart allows a single plain outgoing edge; WorkflowEnd ends the
	// selected path.
	WorkflowStart = "__start__"
	WorkflowEnd   = "__end__"
	// MultiagentFinish is the router completion signal; GuardRoute maps it to
	// WorkflowEnd.
	MultiagentFinish               = "FINISH"
	WorkflowRejectApprovalMismatch = "approval_mismatch"
	WorkflowRejectBeforeApprove    = "before_approve"
	WorkflowRejectResumeInput      = "resume_input"
)
