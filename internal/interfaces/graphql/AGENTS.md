# AGENTS.md

## 条目目录

- [适用范围](#适用范围)
- [接口合同](#接口合同)
- [生成与验证](#生成与验证)

## 适用范围

适用于 `internal/interfaces/graphql/`，继承[后端规则](../../AGENTS.md)。

## 接口合同

- [graph/schema.graphqls](graph/schema.graphqls) 定义外部合同，[graph/inputs.go](graph/inputs.go) 与 [graph/mappers.go](graph/mappers.go) 做输入、输出转换；resolver 将用例编排交给 application。
- [graph/errors.go](graph/errors.go) 统一公开结构化错误；保留 `code`、`category`、`retryable`、`userAction` 和安全的 `details` 语义，避免直接暴露底层错误。
- 查询传递分页、筛选与资源 ID；新增订阅事件的类型和字段必须对应实际变化，遵循根规则的最小载荷边界。
- 订阅发送必须响应上下文取消，避免客户端断开后 goroutine 卡在 channel 发送。

## 生成与验证

- 配置为仓库根 [gqlgen.yml](../../../gqlgen.yml)；在仓库根运行 `go run github.com/99designs/gqlgen generate`，核对 schema、resolver 及生成模型差异。
- 不单独修改 [graph/generated/](graph/generated/) 或 `models_gen.go` 来改变合同。
- 在仓库根运行 `go test ./internal/interfaces/graphql/graph`；映射和转发分别参考 [graph/mappers_test.go](graph/mappers_test.go) 与 [graph/schema_resolvers_test.go](graph/schema_resolvers_test.go)。
