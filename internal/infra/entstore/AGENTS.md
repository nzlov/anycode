# AGENTS.md

## 条目目录

- [适用范围](#适用范围)
- [事务与持久化](#事务与持久化)
- [生成代码](#生成代码)
- [验证](#验证)

## 适用范围

适用于 `internal/infra/entstore/`，继承[后端规则](../../AGENTS.md)。

## 事务与持久化

- [store.go](store.go) 实现数据库选择、迁移和 `UnitOfWork`；事务仓储使用 `tx.Client()`，错误回滚，成功后提交。
- [execution_claim.go](execution_claim.go) 管理执行与关闭的数据库竞争；修改时保持预期状态比较和容量限制的原子性。
- [event_store.go](event_store.go) 拒绝 Codex 对话、用量与压缩内容进入领域事件存储；不要移除这些检查来满足上层写入。
- 数据库同时有 Turso/libSQL 和 PostgreSQL 适配；新增 SQL 或迁移需检查方言差异和重复执行行为。

## 生成代码

- [ent/schema/](ent/schema/) 是 schema 输入，[ent/generate.go](ent/generate.go) 是生成入口；在仓库根运行 `go generate ./internal/infra/entstore/ent`。
- 修改 schema 后核对生成代码和迁移行为；不能只改生成的模型或 builder。

## 验证

- 在仓库根运行 `go test ./internal/infra/entstore`，使用测试独占的临时数据库与数据目录。
- [postgres_integration_test.go](postgres_integration_test.go) 仅在设置 `ANYCODE_TEST_POSTGRES_URL` 时执行 PostgreSQL 集成测试，会创建并删除临时 schema；该变量只能指向测试专用数据库。未设置时说明该项跳过。
- 数据库环境变量优先级和测试实例隔离遵循项目根规则。
