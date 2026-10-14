# AGENTS.md

## 条目目录

- [适用范围](#适用范围)
- [下级规则索引](#下级规则索引)
- [事务与事件](#事务与事件)
- [验证](#验证)

## 适用范围

适用于 `internal/application/`，继承[后端规则](../AGENTS.md)。领域归属和依赖方向以祖先规则为准。

## 下级规则索引

- [卡片执行](session/AGENTS.md)：适用于 `session/`；修改排队、恢复、提问协调或 Side 生命周期时读取。
- [工作流编排](workflow/AGENTS.md)：适用于 `workflow/`；修改节点推进、重试或审批时读取。
- [提问用例](question/AGENTS.md)：适用于 `question/`；修改请求、答案和订阅时读取。
- [时间线](timeline/AGENTS.md)：适用于 `timeline/`；修改历史分页、实时事件或详情读取时读取。

## 事务与事件

- [port/transaction.go](port/transaction.go) 定义 `UnitOfWork`、事务仓储及执行占用端口。需要原子提交的状态、运行记录和领域事件通过同一个 `Tx` 写入，避免在事务回调中改用事务外仓储。
- 领域事件持久化成功并提交事务后再发布；发布失败不能把已提交的操作当成未执行而重复触发副作用。
- `ClaimExecution` 和 `PrepareClose` 返回明确的竞争结果；调用方分别处理过期队列、已有运行、容量不足和关闭竞争，不把所有结果视为成功。
- 用例错误使用 [apperror/](apperror/) 的结构化错误合同；供接口公开的消息和细节与内部错误原因分开。

## 验证

在仓库根运行相关包的 `go test`；事务与事件改动同时覆盖回滚、重复调用和发布失败。依赖方向检查使用 `go test ./internal/architecture`。
