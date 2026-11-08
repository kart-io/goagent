// 管理服务的固定领域信号与其他核心控制信号共同维护；同类错误仍保持独立身份。
package errors

var (
	ErrManagementNotFound         = newSentinel(CodeNotFound, "资源不存在")
	ErrManagementConflict         = newSentinel(CodeConflict, "资源状态或幂等内容冲突")
	ErrManagementInvalid          = newSentinel(CodeInvalidInput, "请求参数无效")
	ErrManagementUnauthenticated  = newSentinel(CodeAccessDenied, "需要可信身份")
	ErrManagementForbidden        = newSentinel(CodeAccessDenied, "没有操作权限")
	ErrManagementUnsupported      = newSentinel(CodeNotImplemented, "不支持所需能力")
	ErrManagementUnavailable      = newSentinel(CodeStoreConnection, "依赖暂不可用")
	ErrManagementApprovalRequired = newSentinel(CodeApprovalRequired, "审批等待必须通过审批决定处理")
	ErrManagementBusy             = newSentinel(CodeOverloaded, "容量已满")
	ErrManagementOwner            = newSentinel(CodeConflict, "执行宿主已被占用")
	ErrManagementAgentDisabled    = newSentinel(CodeWaiting, "Agent已停用，命令等待重新启用")
	ErrManagementUsageMissing     = newSentinel(CodeNotFound, "该运行尚无持久用量证据")
)
