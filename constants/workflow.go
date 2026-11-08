package constants

// 图起止和续跑协议值；领域图与路由接口仍由各自包维护。
const (
	// WorkflowStart 只允许一条普通出边；WorkflowEnd 终止所选路径。
	WorkflowStart = "__start__"
	WorkflowEnd   = "__end__"
	// MultiagentFinish 是路由函数的完成信号，GuardRoute 将其映射为 WorkflowEnd。
	MultiagentFinish               = "FINISH"
	WorkflowRejectApprovalMismatch = "approval_mismatch"
	WorkflowRejectBeforeApprove    = "before_approve"
	WorkflowRejectResumeInput      = "resume_input"
)
