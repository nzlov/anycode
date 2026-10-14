# AGENTS.md

## 条目目录

- [仓库定位](#仓库定位)
- [下级规则索引](#下级规则索引)
- [常用入口](#常用入口)
- [执行规则](#执行规则)
- [状态同步边界](#状态同步边界)
- [DDD 分层](#ddd-分层)
- [安全边界](#安全边界)
- [验证](#验证)

AnyCode 仓库的项目级代理规范。用户当前明确指令优先于本文件。

## 仓库定位

- AnyCode 是 Web 版 Codex agent 工具。
- 后端使用 Go、gqlgen、ent 和 Turso/libSQL；Go module 为 `github.com/nzlov/anycode`。
- 前端使用 Quasar、GraphQL 和 GraphQL WebSocket subscription；构建产物由 Go `embed` 嵌入。

## 下级规则索引

- [后端规则](internal/AGENTS.md)：适用于 `internal/` 下的后端实现与测试；涉及这些文件时读取。
- [前端规则](web/AGENTS.md)：适用于 `web/` 下的界面、服务调用与构建；涉及这些文件时读取。
- 修改目标文件前，检查从项目根到目标目录沿途的 `AGENTS.md`；跨模块任务读取各相关分支，索引链接不代表规则已加载。
- 维护规则文件时同步条目目录和最近上级索引；全项目约束保留在本文件，子级仅补充所在目录的差异。

## 常用入口

- [cmd/anycode/main.go](cmd/anycode/main.go)：服务启动与依赖装配。
- [internal/](internal/)：后端领域、用例、基础设施和接口实现；领域职责见后端规则。
- [web/](web/)：Quasar 前端源码与测试；构建产物嵌入后端静态资源。
- [gqlgen.yml](gqlgen.yml)：GraphQL schema、生成代码和 resolver 路径配置。
- [Makefile](Makefile) 与 [scripts/verify/main.go](scripts/verify/main.go)：统一验证入口与具体检查项。
- [README.md](README.md) 与 [.env.example](.env.example)：部署说明和环境配置示例。

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

## 安全边界

- 访问密钥是服务账号权限范围内的代码执行凭据：持有者既能浏览服务进程可读路径，也能配置 worktree 初始化脚本执行任意 shell。不得弱化 HTTP、WebSocket、MCP、附件、下载或 Diff 的鉴权边界。
- GraphQL 不返回裸磁盘路径；传给 Codex 的附件路径只能由后端从已归档附件映射生成。
- 只允许清理 AnyCode 创建的 worktree，永不删除用户原始项目目录；当前卡片工作树由 AnyCode 管理，不得删除、移动、重建或手动清理。
- worktree 路径由后端生成，不能直接拼接用户输入；目录浏览权限由服务进程权限和访问密钥决定，不在代码中硬编码为 `/workspaces`。

## 验证

- 测试范围与风险匹配；文档改动至少运行 `git diff --check`。
- 启动 AnyCode 测试实例时，必须使用该测试独占的临时数据库和数据目录，不得继承、连接或迁移主服务数据库。使用本地 Turso/libSQL 时，注意 `DATABASE_URL` 优先于 `TURSO_DATABASE_URL`：测试命令必须清除继承的 `DATABASE_URL` 和 `TURSO_AUTH_TOKEN`，并将 `TURSO_DATABASE_URL`、`ANYCODE_DATA_DIR` 显式指向临时目录；启动后先核对实际数据库目标，再执行迁移、恢复或浏览器测试。
- 常规收口统一运行 `make verify`；该目标并行执行后端测试与静态检查、前端测试、类型检查、生产构建和 Git 差异检查；具体检查项以 `scripts/verify/main.go` 为准。
- 验证失败时记录结果，继续修复或询问用户。
