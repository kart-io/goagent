# 真实服务验证

`services_test.go` 测试真实 PostgreSQL、Redis 命名空间 CRUD、隔离、取消，以及连接重建后的持久化和 Redis 检查点。NATS 测试位于 multiagent，Qdrant 的业务 ID 与元数据回归位于 retrieval。

所有新测试只读取显式的测试端点；未提供时会显示 skip，不会被计为通过。请使用隔离测试服务。PostgreSQL 使用随机表名，Redis 使用随机前缀，Qdrant 新回归使用随机集合，并注册清理。

```sh
export GOAGENT_TEST_POSTGRES_DSN='host=127.0.0.1 port=15432 user=goagent password=test-only dbname=goagent_test sslmode=disable'
export GOAGENT_TEST_REDIS_ADDR=127.0.0.1:16379
export GOAGENT_TEST_NATS_URL=nats://127.0.0.1:14222
export GOAGENT_TEST_QDRANT_ADDR=127.0.0.1:6334

go test -race ./integration ./multiagent ./retrieval \
  -run 'TestReal|TestQdrantMetadata|TestQdrantPointID' -count=1 -timeout 120s
```

环境地址需对应自行启动的测试实例。本轮实际验证服务版本为 PostgreSQL 16-alpine、Redis 7-alpine、NATS 2.11.10-alpine 和 Qdrant 1.16.2。没有测试多副本一致性、跨主机接管或持久化消息消费。

仓库旧 Qdrant 集成测试仍使用 localhost:6334 及固定测试集合名。运行不带 `-short` 的整仓测试时，此端口应为专用临时 Qdrant，避免复用业务实例。

## 全量验证

使用与 GOROOT 一致的 Go 二进制和 PATH。临时测试端点按上文配置后：

```sh
# DeepSeek 示例会在配置真实密钥时调用线上服务；离线回归移除该变量。
env -u DEEPSEEK_API_KEY GO_TEST_MODE=true GOMAXPROCS=2 \
  TEST_PDF_PATH="$PWD/examples/basic/16-pdf-rag/cymbal-starlight-2024.pdf" \
  go test -json -p 2 -race -covermode=atomic \
    -coverprofile=/tmp/goagent-full.coverage.out -count=1 -timeout 300s ./... \
    > /tmp/goagent-full.jsonl

go vet ./...
go build ./...
go tool cover -html=/tmp/goagent-full.coverage.out -o /tmp/goagent-coverage.html
```

查看 JSON 中的 pass、fail、skip，应分别统计顶层与子用例，不能将子用例当作独立业务样本。示例进程正常退出只属于冒烟验证，不等于模型效果或生产验收。

完整结果、修复和剩余跳过项见[本轮验证报告](../docs/evaluation/2026-10-26-comprehensive-validation.md)。
