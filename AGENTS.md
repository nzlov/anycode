# AGENTS.md

AnyCode 仓库的项目级代理规范。用户当前明确指令优先于本文件。

## 仓库定位

- AnyCode 是 Web 版 Codex agent 工具。
- 后端使用 Go、gqlgen、ent 和 Turso/libSQL；Go module 为 `github.com/nzlov/anycode`。
- 前端使用 Quasar、GraphQL 和 GraphQL WebSocket subscription；构建产物由 Go `embed` 嵌入。

## 执行规则

- 需求存在会改变架构或行为边界的歧义时，先向用户确认。
- 只修改当前目标必需的内容，不顺手重构、清理或格式化无关代码。
- 优先使用标准库、现有依赖和现有领域边界；不为能在调用处清楚表达的单次逻辑增加 helper、wrapper、adapter 或配置项。
- 出现重复 mapping、透传 wrapper 或其他胶水代码时，先检查职责和数据形状是否放错边界。无法避免的胶水代码用简短 `GLUE:` 注释说明边界、原因和删除条件。

## 状态同步边界

- 禁止以“投影（projection）”“镜像（mirror）”“影子状态”“前端副本”或 read model 等概念增加第二套可独立演进的状态结构、缓存或同步链路。
- 查询负责读取当前状态；订阅只发送变化语义、资源 ID 和本次变化所需的最小数据。简单变化直接更新已有对象的对应字段，关联字段较多的变化按资源 ID 发起针对性查询。
- 禁止为了状态同步而在 application、GraphQL 或前端增加全量对象透传 wrapper、重复 mapping、常驻全局状态容器或双向同步逻辑；确有必要时必须先说明边界问题、最小替代方案和改动范围，并取得用户明确同意。
- 禁止用订阅全量推送对象来掩盖事件合同不清；事件类型必须表达实际变化，载荷与该事件严格对应。

## DDD 分层

- `internal/domain/*` 只放领域模型、值对象、领域服务和端口接口，不依赖 gqlgen、ent、HTTP、Quasar 或具体 CLI 包。
- `internal/application` 编排用例；`internal/infra/*` 实现存储、Codex、MCP、git 和文件系统等外部适配。
- `internal/interfaces/graphql` 只做 GraphQL schema、resolver 和 DTO 转换；resolver 保持薄，不放业务规则。
- ent schema 与生成代码放在 `internal/infra/entstore/ent`；gqlgen 生成代码放在 `internal/interfaces/graphql/graph`。
- ent model 和 GraphQL model 不作为领域模型，不跨层穿透；通过 repository、application DTO 或领域端口转换。
- Codex 命令行参数只在 `internal/infra/codexcli` 拼装。
- 新增领域对象和用例前，先按上述分层与领域边界确定归属，不新增跨层 glue package。

## DDD 领域边界

- `project` 管理项目身份、路径、Git 探测结果、默认流程和 worktree 初始化配置；只表达 Git 事实，不执行 Git 命令。
- `session` 管理卡片需求、模式、状态机、执行配置、队列意图、附件及 worktree 清理所有权；不推进流程节点。
- `workflow` 管理流程定义、运行、节点尝试、条件、重试和审批规则；跨节点推进由 application runner 编排。
- `question` 管理 `answer_user` 问题批次、选项、答案和交付状态；不直接启动、恢复或停止进程。
- `process` 管理 Codex 运行记录、生命周期、能力和标准化事件，并定义 Codex 进程端口；CLI 与操作系统进程实现在 infra。
- `gitdiff` 管理 worktree、Diff、提交历史和合并的数据合同及端口；Git CLI 副作用实现在 infra。
- `setting` 管理可复用快捷命令；`auth` 只表达访问主体；`event` 管理可持久化领域事件及提交后发布合同。
- 跨上下文编排放在 application；各上下文保留自己的 ID 类型，通过 application use case 或明确端口交换数据，不共享 ent、GraphQL DTO 或 infra 实现。

## 安全边界

- 访问密钥是服务账号权限范围内的代码执行凭据：持有者既能浏览服务进程可读路径，也能配置 worktree 初始化脚本执行任意 shell。不得弱化 HTTP、WebSocket、MCP、附件、下载或 Diff 的鉴权边界。
- GraphQL 不返回裸磁盘路径；传给 Codex 的附件路径只能由后端从已归档附件映射生成。
- 只允许清理 AnyCode 创建的 worktree，永不删除用户原始项目目录；当前卡片工作树由 AnyCode 管理，不得删除、移动、重建或手动清理。
- worktree 路径由后端生成，不能直接拼接用户输入；目录浏览权限由服务进程权限和访问密钥决定，不在代码中硬编码为 `/workspaces`。

## 前端约束

- 优先使用现有 Quasar 组件和统一图标集，遵循已有页面与响应式布局模式。
- 操作界面保持紧凑、可扫描；交互改动必须覆盖 hover、focus 和移动端溢出检查。

## 验证

- 测试范围与风险匹配；文档改动至少运行 `git diff --check`。
- 常规收口统一运行 `make verify`；该目标依次执行后端测试与静态检查、前端测试、类型检查、生产构建和 Git 差异检查。
- 验证失败时记录结果，继续修复或询问用户。
