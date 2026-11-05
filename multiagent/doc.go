// Package multiagent 在 workflow 之上提供多 Agent 预置模式：Sequential、Loop、RouterSupervisor
// 三个图构造器，ParallelStep 组合件，工具型 Supervisor（SupervisorTools）与文本接力便捷层。
//
// 本包与 v0.17 删除的旧 multiagent 包同名，但 API 与语义不兼容，旧代码不能直接迁移；
// 它不引入运行时、调度器或消息投递，所有执行仍走 workflow、builder 与 tools。
//
// 构造器返回的 *workflow.Graph 只应直接交给 Compile 或 CompileDurable：构造器已按拓扑算好 MaxSteps，
// 继续 AddNode 不会报错，但步数上限不再准确。需要组合时用 ParallelStep，
// 或用 workflow.NewGraph 手写拓扑并以 AddStep、GuardRoute 复用本包的 Step 与兜底路由。
package multiagent
