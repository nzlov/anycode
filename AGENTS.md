# AGENTS.md

本文件是 AnyCode 仓库的项目级代理规范。若与用户当前明确指令冲突，以用户当前指令为准。

## 交流规则

- 每次直接回复用户时，开头必须先称呼：`主人`。
- 不发送可有可无的过程说明；需要说明时只说当前动作、依据、风险或验证结果。
- 需求不清楚时先停下来问，不要自行选择会改变架构或行为边界的解释。

## 项目目标

AnyCode 是一个 Web 版 Codex agent 工具：

- 后端：Go、gqlgen、ent、Turso/libSQL。
- Go module：`github.com/nzlov/anycode`。
- 前端：Quasar 标准框架，GraphQL，GraphQL WebSocket subscription。
- 部署：Docker Compose，默认挂载 `./workspaces:/workspaces`。
- 产物：前端 build 后通过 Go `embed` 嵌入后端二进制。
- 协议：MIT，版权归 `nzlov`。

## 当前已确认的产品边界

- 使用本机 `codex` CLI。
- 允许多项目、多会话；每个运行中的会话卡片对应一个 Codex 进程。
- 卡片创建时用户选择基础分支；如果项目是 git 仓库，则基于基础分支创建独立 worktree。
- 卡片创建时用户选择模式：流程模式或会话模式。
- 新建卡片必须允许配置 Codex 模型、思考强度、运行权限和文件附件。
- 项目可配置流程图；流程模式卡片创建后按项目流程图自动推进。
- 只有流程模式走项目流程图；会话模式不读取、不执行项目流程图。
- 两种模式的卡片点击后都打开会话页面。
- 流程节点由用户配置，节点可配置标题、提示词、失败重试、人工审批。
- 条件分支使用 JSON 布尔 AST，不执行用户脚本。
- MCP 暴露 `answer_user` 工具，用于 agent 向用户提问。
- `answer_user` 问题类型为选项模式：agent 给出若干选项，UI 允许用户选择选项，也允许勾选“自定义答案”并输入自己的回答。
- 前端问题弹窗使用 tab；同一批问题全部回答后一次性提交，Codex 进程继续。
- 项目目录选择器由前端页面实现，展示后端返回的目录树。
- 后端目录浏览不限制路径范围；访问边界由服务进程权限与访问密钥决定。
- 访问控制只使用环境变量配置的访问密钥。

## 工作树规则

- 纯文档和计划更新可以在当前 checkout 中完成。
- 一旦修改源码、测试、脚本、迁移、生成输入或运行时配置，先从目标分支创建独立 git worktree。
- worktree 放在项目内 `.worktree/<task-name>`，不要放到 `/tmp`。
- 不要把无关任务混在同一个 worktree。
- 不要回滚用户已有改动；遇到相关未提交改动时，先读懂再继续。

## 计划与验证

- 在用户明确要求“开始开发”前，只同步修改计划和原型图，不进入代码实现。
- 多步骤任务先维护 `docs/plan/` 下的 Markdown TODO 计划。
- TODO 必须是可执行动作，并带明确 `verify:` 检查。
- 只有验证已通过，才能把计划项改成 `[x]`。
- 失败时保持未勾选，记录失败结果，再修复或询问。
- 常规收口检查优先使用：
  - `go test ./...`
  - `npm --prefix web run build`
  - `git diff --check`

## DDD 分层规范

- `internal/domain/*` 只放领域模型、值对象、领域服务和端口接口；不要 import gqlgen、ent、HTTP、Quasar 或具体 CLI 包。
- `internal/application` 负责编排用例，例如创建项目、创建卡片、启动流程、提交答案、停止会话。
- `internal/infra/*` 负责外部适配：ent 存储、Turso/libSQL、Codex CLI、MCP、git CLI、文件系统浏览。
- `internal/interfaces/graphql` 只负责 GraphQL schema/resolver/DTO 转换；resolver 必须薄，不放业务规则。
- ent 生成代码与 schema 放在 `internal/infra/entstore/ent` 下，不放仓库根目录。
- gqlgen 生成代码放在 `internal/interfaces/graphql/graph` 下，不放仓库根目录。
- ent model 不直接穿透到领域层或前端；通过 repository/application DTO 转换。
- GraphQL 输入输出模型不作为领域模型使用。

## 领域边界

- `Project`：项目路径、显示名、是否 git 仓库、默认流程、项目级配置。
- `Session`：卡片需求、模式、状态、最近运行时间、Codex 运行状态、worktree 信息。
- `Workflow`：流程定义、节点、边、条件、重试、审批、运行状态、节点运行记录。
- `Question`：`answer_user` 产生的问题批次、待回答状态、答案提交、恢复信号。
- `Git` 端口：分支列表、repo 检测、worktree 创建。
- `Process` 端口：Codex 进程启动、停止、事件解析、状态发布。
- `Auth`：HTTP、GraphQL、WebSocket、MCP 内部调用的访问密钥鉴权。

## 流程图实现规则

- 前端使用 Vue Flow 做编辑器，但存储时转换为业务 JSON。
- 不把 Vue Flow 的临时 UI 字段、坐标以外的组件状态泄漏进领域模型。
- 条件分支使用 JSON AST，第一版支持 `eq`、`ne`、`contains`、`exists`、`gt`、`gte`、`lt`、`lte`、`all`、`any`、`not`。
- runner 按边的 `priority` 从小到大选择第一个命中的条件。
- 无条件命中时流程进入 `blocked`，并把阻塞原因推送给前端。
- 失败重试优先于出边选择；超过重试次数后再走失败分支或进入 blocked。
- 人工审批必须持久化，不能只放内存。

## Codex 与 MCP 规则

- 每个运行卡片启动一个 `codex exec --json -C <workdir>` 进程。
- git 项目的 `<workdir>` 是卡片 worktree；非 git 项目使用项目目录。
- 流程模式由 workflow runner 编排；会话模式直接运行 Codex 会话，不创建 `WorkflowRun`。
- Codex 启动参数必须来自会话配置：模型、思考强度、权限、文件附件和工作目录。
- Codex JSONL 事件解析在 `codexcli` adapter 中完成；未知事件保留 raw payload。
- `answer_user` MCP handler 创建问题批次后阻塞等待 UI 提交答案。
- UI 提交完整批次的选项/自定义答案后，MCP handler 返回结构化答案给 Codex。
- 停止会话时必须清理进程，并发布 WebSocket 状态事件。

## GraphQL 与实时状态

- mutation 负责命令：创建项目、创建卡片、启动/停止会话、提交答案、保存流程图。
- query 负责读取：项目列表、会话卡片、流程定义、问题批次。
- 所有列表分页、过滤、排序和 total 都由后端 GraphQL/application 计算；前端不能拉全量数据后本地分页过滤。
- subscription 负责实时事件：会话状态、当前节点、待回答、回答完成、进程退出。
- HTTP 和 WebSocket 必须使用同一访问密钥规则。

## 前端规范

- 第一屏是实际工作台，不做 landing page。
- 左侧是总揽按钮和项目列表；中间是会话卡片列表。
- 点击总揽显示跨项目卡片列表，不能按项目分组。
- 项目条目右侧有设置图标，点击后弹出菜单，菜单项包含“流程配置”。
- 卡片分为最近和历史；最近定义为最近 3 天内运行过。
- 历史区显示近 7 天运行记录，标题后有“更多”入口进入后端分页过滤的会话表格。
- 卡片必须清晰显示用户需求摘要、运行中/已停止、当前流程节点、待回答状态。
- 会话详情页必须提供“查看 Diff”按钮，打开类似 GitHub PR diff 的页面查看该卡片 worktree 变更；Diff 页必须支持“单个文件”和“全部 Diff”展示类型，diff 数据由后端计算。
- 新建卡片弹窗的需求输入区采用类似 Codex 的提示词编辑器形态，使用中性浅色开发工具配色；项目、分支、模式集中在顶部。附件列表放在提示词正文上方，支持任意文件类型，图片和视频附件可点击预览，每个附件可删除。底部只保留权限图标、模型下拉、思考强度下拉和一个“创建卡片”提交入口；不要出现全屏按钮、发送按钮、底部取消按钮、底部模式说明卡片或大附件卡片列表。
- 卡片列表顶部不放“新卡片”等操作按钮；新建卡片使用右下角 FAB。
- 响应式布局必须覆盖手机端：顶部栏、抽屉式侧边栏、单列卡片列表、右下 FAB。
- 使用 Quasar 组件优先；图标使用统一图标集，不使用 emoji 图标。
- 操作型界面保持高密度、克制、可扫描；不要做装饰性大卡片或营销式 hero。
- 所有可点击元素要有清晰 hover/focus 状态，移动端不能出现文字溢出或横向滚动。

## 数据与配置

- 数据库通过环境变量配置，允许本地 libSQL 或远程 Turso。
- 访问密钥通过 `ANYCODE_ACCESS_KEY` 配置。
- HTTP 地址通过 `ANYCODE_HTTP_ADDR` 配置，默认 `:8080`。
- Codex 可执行文件通过 `CODEX_BIN` 配置，默认 `codex`。
- 应用数据目录通过 `ANYCODE_DATA_DIR` 配置。

## Docker 规则

- Compose 默认提供 `./workspaces:/workspaces` 和 `./data:/data` 挂载。
- 不在代码中把目录选择限制为 `/workspaces`。
- 由于目录浏览不限制范围，部署文档必须明确：持有访问密钥的人可以浏览后端进程可读路径。

## 简单优先

- 不为尚未需要的部署模式、权限模型、流程脚本语言或插件系统提前抽象。
- 优先使用标准库、项目已有依赖和明确领域端口。
- 不新增只透传数据的 wrapper、adapter 或重复 mapping。
- 如果实现开始依赖大量胶水代码，先回到领域边界检查数据形状和职责归属。
