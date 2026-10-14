# AGENTS.md

## 条目目录

- [适用范围](#适用范围)
- [节点与审批](#节点与审批)
- [验证](#验证)

## 适用范围

适用于 `internal/application/workflow/`，继承[应用层规则](../AGENTS.md)。

## 节点与审批

- [usecase.go](usecase.go) 根据领域图、条件和运行状态返回推进结果；跨卡片执行副作用由调用方协调。
- before-run 审批通过前不执行系统节点副作用、不创建占位节点运行；after-run 审批与节点输出数据分别处理。
- 恢复已有节点使用已有尝试，重跑创建下一次尝试并遵守重试上限；对旧节点完成通知和重复恢复使用已持久化的推进结果。
- 缺少结果、无匹配边、执行失败和等待审批是不同路径；不要以默认成功跳过结果验证或条件判断。
- 审批保留命名空间由领域图合同管理，不能混入 agent 自定义输出字段。

## 验证

在仓库根运行 `go test ./internal/application/workflow ./internal/domain/workflow`。参考 [usecase_test.go](usecase_test.go) 的审批前副作用、恢复幂等、尝试上限和结果合同用例。
