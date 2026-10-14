# AGENTS.md

## 条目目录

- [适用范围](#适用范围)
- [执行与恢复](#执行与恢复)
- [提问与旁支](#提问与旁支)
- [验证](#验证)

## 适用范围

适用于 `internal/application/session/`，继承[应用层规则](../AGENTS.md)。

## 执行与恢复

- [usecase.go](usecase.go) 负责卡片的执行协调，[locker.go](locker.go) 提供卡片锁；排队、启动和关闭继续使用已有锁及事务占用合同。
- 恢复操作需要在同一事务中处理旧运行、队列意图和事件；重新读取并比较持久化状态，避免覆盖恢复快照之后创建的新运行。
- 恢复工作流卡片时保留当前节点尝试；恢复已有尝试与重跑新尝试由 workflow 用例区分。
- 关闭与恢复覆盖已准备关闭、仍有待回答问题和停止中状态；不能因订阅断开就关闭卡片。

## 提问与旁支

- 动态提问使用稳定请求 ID 和来源运行关联；当前 turn 等待答案，原运行结束后由持久化请求协调恢复，避免答案丢失或重复排队。
- 等待答案前先订阅变化，再读取请求当前状态，覆盖答案已经提交的情况；等待期间不持有卡片锁。
- [side.go](side.go) 与 [side_runtime.go](side_runtime.go) 管理 Side 的独立线程与记录；覆盖参数及附件不改变源卡片，订阅断开不终止 Side。
- 关闭卡片时先停止运行中的 Side 再删除记录；具体恢复和清理行为以现有测试为基线。

## 验证

在仓库根运行 `go test ./internal/application/session`。恢复改动重点检查 [recovery_test.go](recovery_test.go)，追加提示去重检查 [prompt_rebuild_test.go](prompt_rebuild_test.go)，旁支改动检查 [side_test.go](side_test.go)。
