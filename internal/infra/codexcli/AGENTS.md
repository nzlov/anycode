# AGENTS.md

## 条目目录

- [适用范围](#适用范围)
- [运行协议](#运行协议)
- [历史与事件](#历史与事件)
- [验证](#验证)

## 适用范围

适用于 `internal/infra/codexcli/`，继承[后端规则](../../AGENTS.md)。

## 运行协议

- [runtime.go](runtime.go)、[appserver.go](appserver.go) 和 [protocol.go](protocol.go) 处理 App Server 协议；对外使用 process 领域合同。
- 动态工具返回到发起请求的原 turn；steer、resume 和 fork 保持运行、线程、turn 的正确关联，resume 仍需注册动态工具。
- compaction 通知不代表运行完成；停止、关闭事件流和迟到事件分别处理，避免重复完成运行。
- 权限模式、配置的可写根和产物目录需要同时体现在启动参数和线程权限中；计划模式也保留 AnyCode 开发者指令。
- Side 临时附件按线程生命周期保留，覆盖两次 turn 之间的使用，再随线程清理。

## 历史与事件

- [session_history.go](session_history.go) 从 Codex 会话文件按偏移读取历史；大内容延迟加载时保留事件与偏移关联。
- [session_events.go](session_events.go) 与 [direct_events.go](direct_events.go) 处理事件转换；保留命令、工具调用和完成事件的关联与去重语义。
- 修改事件解析时同时检查实时与历史路径，不能将中间等待消息误作最终命令结果。

## 验证

在仓库根运行 `go test ./internal/infra/codexcli`。沿用现有协议模拟测试；重点覆盖 [runtime_test.go](runtime_test.go)、[session_events_test.go](session_events_test.go) 和 [side_test.go](side_test.go)。
