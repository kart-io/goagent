# 可复用退款业务保护

`refund.Service` 从退款恢复案例提取，只有标准库依赖。宿主注入 `Store`、`Authorizer` 和 `Policy`，缺少任一项拒绝执行。

- `Existing` / `Register`：业务 RequestID 去重，同 ID 不同订单或金额报错。重试保留首次审查的事实与期限。
- `Approve`：核对注册快照、授权、到期时间、当前订单版本和全部金额/币种/交付事实。
- `Refund`：先返回已提交回执；新效果重新检查授权与事实，在同一事务内检查累计退款额度并写入回执。提交失败不返回成功回执。

`Store.Transaction` 必须跨请求、跨连接串行化同一订单的写入，回调失败回滚，成功后原子提交。仅锁 RequestID 不够，因为不同请求可能退款同一订单。`Transaction` 的缺失记录统一返回 `ErrNotFound`，存储必须有业务请求和回执唯一约束。[SQLite 适配器](../../examples/workflow/refund-recovery/ledger.go)使用即时写事务；原有跨进程崩溃和并发超额测试继续覆盖此接口。

```go
service := &refund.Service{
    Store: repository,
    Authorizer: refund.AuthorizeFunc(authorizeAuthenticatedPrincipal),
    Policy: checkBusinessRefundPolicy,
}
registered, err := service.Register(ctx, reviewedRequest)
if err != nil { return err }
if err := service.Approve(ctx, registered, authenticatedPrincipalID); err != nil { return err }
receipt, err := service.Refund(ctx, workflow.DurableKey(ctx), registered)
```

宿主必须先认证调用者，授权策略负责权限与订单归属；租户的请求 ID 和订单 ID 应在宿主命名空间内唯一。`Policy` 决定退款期限等业务条件，组件固定执行正金额、币种、有效版本、审批过期和事实一致性检查。审核期限由可信宿主生成，不能直接使用用户提交的期限。现有案例仍注入七天合成规则与 `demo-reviewer`，不代表接入真实身份系统。

这是**本地事务效果账本**，不调用支付网关。回执保留原案例的 `demo-refund-` 前缀以保持兼容；不是支付机构回单。真实支付必须通过支付方业务幂等键和对账/补偿协议接入，不能在 SQL 事务中随意加入不可回滚的远程扣款。

运行验证：

```sh
go test -race ./workflow/refund ./examples/workflow/refund-recovery
```
