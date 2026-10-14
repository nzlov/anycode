# AGENTS.md

## 条目目录

- [适用范围](#适用范围)
- [请求与答案](#请求与答案)
- [验证](#验证)

## 适用范围

适用于 `internal/application/question/`，继承[应用层规则](../AGENTS.md)。

## 请求与答案

- [usecase.go](usecase.go) 管理请求持久化、答案校验和待回答请求通知；进程等待与恢复由 session 协调。
- 稳定请求 ID 重复创建只复用符合现有状态规则的请求；提交必须包含完整答案，同一答案重复提交保持幂等，已回答请求不能被不同答案覆盖。
- 更新订阅随调用上下文结束，突发变化不能静默丢失；取消请求需要让等待方收到状态变化。
- 等待时间观测只记录指标，不把问题或答案正文放入观测数据。

## 验证

在仓库根运行 `go test ./internal/application/question ./internal/domain/question`；涉及答案恢复时同时运行 `go test ./internal/application/session`。
