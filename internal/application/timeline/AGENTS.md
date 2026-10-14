# AGENTS.md

## 条目目录

- [适用范围](#适用范围)
- [历史与实时事件](#历史与实时事件)
- [验证](#验证)

## 适用范围

适用于 `internal/application/timeline/`，继承[应用层规则](../AGENTS.md)。

## 历史与实时事件

- [usecase.go](usecase.go) 通过 `CodexHistory` 读取当前线程历史，通过 `LiveEventSource` 接收实时事件；领域事件存储用于相应的持久化事件，不能成为 Codex 对话内容的第二份存储。
- 仅在没有线程时按现有规则回退到持久化事件；历史读取错误需要返回，不能用回退掩盖故障。
- 分页保留游标、条数限制和角色筛选语义；事件详情读取验证当前线程、事件 ID 与字节偏移的关联。
- 历史和详情中的图片通过已归档文件解析；过滤常规生命周期噪声时保留失败退出等有效信息。

## 验证

在仓库根运行 `go test ./internal/application/timeline`；历史协议改动同时验证 `go test ./internal/infra/codexcli`。行为入口见 [usecase_test.go](usecase_test.go)。
