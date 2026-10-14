# AGENTS.md

## 条目目录

- [适用范围](#适用范围)
- [DDD 领域边界](#ddd-领域边界)
- [下级规则索引](#下级规则索引)
- [验证](#验证)

## 适用范围

适用于 `internal/` 及其子目录，继承[项目根规则](../AGENTS.md)。全项目的分层、状态同步和安全边界见根规则。

## DDD 领域边界

- `project` 管理项目身份、路径、Git 探测结果、默认流程和 worktree 初始化配置；只表达 Git 事实，不执行 Git 命令。
- `session` 管理卡片需求、模式、状态机、执行配置、队列意图、附件及 worktree 清理所有权；不推进流程节点。
- `workflow` 管理流程定义、运行、节点尝试、条件、重试和审批规则；跨节点推进由 application runner 编排。
- `question` 管理 `questions` 请求、选项和答案；不直接启动、恢复或停止进程。
- `process` 管理 Codex 运行记录、生命周期、能力和标准化事件，并定义 Codex 进程端口；CLI 与操作系统进程实现在 infra。
- `gitdiff` 管理 worktree、Diff、提交历史和合并的数据合同及端口；Git CLI 副作用实现在 infra。
- `setting` 管理可复用快捷命令；`auth` 只表达访问主体；`event` 管理可持久化领域事件及提交后发布合同。
- 跨上下文编排放在 application；各上下文保留自己的 ID 类型，通过 application use case 或明确端口交换数据，不共享 ent、GraphQL DTO 或 infra 实现。

## 下级规则索引

- [应用编排](application/AGENTS.md)：适用于 `application/`；修改用例、事务或跨领域协调时读取，并按其索引继续深入。
- [数据库存储](infra/entstore/AGENTS.md)：适用于 `infra/entstore/`；修改仓储、迁移或 ent schema 时读取。
- [Codex 适配](infra/codexcli/AGENTS.md)：适用于 `infra/codexcli/`；修改运行协议、动态工具或历史解析时读取。
- [Git 适配](infra/gitcli/AGENTS.md)：适用于 `infra/gitcli/`；修改分支、工作树、归属检查或快照时读取。
- [GraphQL 接口](interfaces/graphql/AGENTS.md)：适用于 `interfaces/graphql/`；修改 schema、resolver、转换或订阅时读取。
- [HTTP 接口](interfaces/http/AGENTS.md)：适用于 `interfaces/http/`；修改路由、鉴权、文件传输或静态资源时读取。

## 验证

以下命令在仓库根运行：

- 修改对应包后运行针对性测试，例如 `go test ./internal/application/session/...`。
- 调整依赖或分层后运行 `go test ./internal/architecture`，核对导入边界。
- 后端整体检查为 `go test ./...` 和 `go vet ./...`；常规收口执行根规则中的 `make verify`。
- 启动测试实例前遵循根规则的临时数据库与数据目录隔离要求。
