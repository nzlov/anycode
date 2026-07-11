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
- 项目可配置多行工作树初始化命令；git 项目卡片创建 worktree 后、附件归档与 Codex/Workflow 启动前在该 worktree 中执行 shell 脚本。
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
- 具体领域对象、端口和应用用例以 `docs/plan/ddd-codex-agent-tool.md` 的“分层领域抽象”为准；实现时先放入对应上下文，不新增跨层 glue package。

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
- 同一 session 同一时间最多允许一个运行中的 Codex 进程；重复启动必须返回当前状态，不能创建第二个进程。
- 流程模式由 workflow runner 编排；会话模式直接运行 Codex 会话，不创建 `WorkflowRun`。
- Codex 启动参数必须来自会话配置：模型、思考强度、权限、文件附件和工作目录。
- Codex JSONL 事件解析在 `codexcli` adapter 中完成；未知事件保留原始 type，并直接使用原始 payload，不增加 `raw` 包装。
- `answer_user` MCP handler 创建问题批次后阻塞等待 UI 提交答案。
- UI 提交完整批次的选项/自定义答案后，MCP handler 返回结构化答案给 Codex。
- 停止会话时必须标记 stopping、终止进程、释放阻塞中的 `answer_user` 等待、记录 exit 状态，并发布 WebSocket 状态事件。
- 服务重启后，有 `codexSessionID` 但无本地进程的会话必须显示可恢复状态；恢复通过 `codex exec resume <id>` 完成。
- `codex exec resume <id>` 失败时必须记录 `resume_failed` 事件并把 session 标记为 `resume_failed`；不能自动跳过节点，也不能静默改用普通 `codex exec`。
- 恢复失败后 UI 必须提供重试恢复、从当前节点重新运行、停止会话三个动作。
- 卡片详情页必须允许用户主动停止和追加描述；追加描述要持久化，用于恢复失败后重造 Codex 会话 prompt。

## 流程节点与进程生命周期

- workflow runner 是流程模式唯一的节点推进者；GraphQL resolver 不能直接推进节点。
- 每个可执行节点创建 `NodeRun` attempt；需要 Codex 时再创建 `ProcessRun`，并把 `ProcessRun` 绑定到当前 `NodeRun`。
- 人工审批前置状态不启动 Codex；审批通过后才启动当前节点进程。
- 当前节点处于 `running`、`waiting_user`、`stopping` 时不能计算下一条边。
- `answer_user` 期间 Codex 进程仍归属当前节点；提交完整答案后同一进程继续。
- Codex 正常退出并产出节点结果后，`NodeRun` 标记 `succeeded`，runner 才按边 priority 评估下一节点。
- Codex 启动失败、异常退出、附件准备失败或权限配置失败时，`NodeRun` 标记 `failed`；runner 先执行 retry，超过 retry 后再走失败分支或 blocked。
- 如果节点结果已持久化但出边未计算，服务恢复后直接基于持久化结果继续评估出边，不需要恢复 Codex 进程。
- 如果节点运行中服务重启，恢复成功后继续归属同一个 `NodeRun`；恢复失败时 workflow run 进入 `waiting_resume_action`，当前节点保持不变。
- 用户选择“重试恢复”不增加 attempt；选择“从当前节点重新运行”创建新的 `NodeRun` attempt，并受 retry 限制。
- 会话模式没有 `WorkflowRun`/`NodeRun`，恢复失败只影响 session，由用户选择重试恢复、重新运行会话或停止。

## GraphQL 与实时状态

- mutation 负责命令：创建/更新项目、创建卡片、启动/停止会话、提交答案、保存流程图。
- query 负责读取：项目列表、会话卡片、流程定义、问题批次。
- 所有列表分页、过滤、排序和 total 都由后端 GraphQL/application 计算；前端不能拉全量数据后本地分页过滤。
- subscription 负责实时事件：会话状态、当前节点、待回答、回答完成、进程退出。
- HTTP 和 WebSocket 必须使用同一访问密钥规则。
- 前端刷新或 WebSocket 断线重连后，必须先 query 当前状态快照，再重新订阅；subscription 事件只做增量更新，不能作为唯一状态来源。
- subscription 事件必须包含可排序的事件 id 或创建时间，用于忽略旧事件和补齐断线期间状态。
- 状态变更、`ProcessEvent`、节点输出和待发布事件必须在同一事务写入；WebSocket 发布发生在事务提交之后，发布失败不能回滚已提交状态。
- GraphQL resolver 只做 DTO 转换和 use case 调用，不能拼接 Codex 参数、直接执行 git 命令、直接读写附件文件或推进 workflow 节点。

## 附件与文件访问

- 新建卡片附件先进入 staged 状态；创建卡片成功后归档为 `SessionAttachment`。
- staged 附件删除必须同时删除临时文件和暂存记录；创建失败必须清理本次未归档附件。
- 附件支持任意文件类型；图片和视频通过后端鉴权 URL 预览，其他文件只展示元数据。
- GraphQL 只返回附件元数据和鉴权预览/下载入口，不能返回裸磁盘路径。
- 附件预览、文件下载、Diff 文件读取必须使用同一访问密钥鉴权，不能通过未鉴权静态目录暴露。
- 传给 Codex 的附件路径只能由后端从已归档附件映射生成。
- 附件永不过期；staged 附件、归档附件和会话附件都保留，除非用户显式删除附件。

## Worktree 规则

- git 项目卡片 worktree 默认创建在 `ANYCODE_DATA_DIR/worktrees/<projectID>/<sessionID>`。
- worktree 路径只能由后端生成，不能直接拼接用户输入。
- 项目工作树初始化命令为空或只含空白时跳过；非 git 项目不执行该命令。保存时保留用户输入的原始多行内容。
- 工作树初始化命令通过 `/bin/sh -c` 执行，以新建 worktree 为工作目录；执行时机在 worktree 创建和基础提交记录成功后、附件归档与 Codex/Workflow 启动前。
- 持有访问密钥的人可以保存任意 shell，并在后续创建 git 卡片时以 AnyCode 服务账号权限执行；这等同于该服务账号权限范围内的远程代码执行，可读取其可访问文件和其他凭据。
- 初始化脚本运行在独立进程组；操作父 context 保留请求 values 但不直接继承请求取消，执行 context 与终止等待 context 是它的两个子 context。请求取消只桥接到执行 context，终止等待 context 使用独立超时。
- 初始化脚本启动失败或非零退出时，持久化 `session.worktree_init_failed` 事件和限长结果，但不改变 session 状态、不删除 worktree，也不阻止附件归档或 Codex/Workflow 启动；脚本已产生的文件变更继续保留在 worktree 中。
- 请求本身取消不属于可忽略的初始化失败；终止并等待脚本进程组后，创建请求按取消处理。
- 非 git 项目不创建 worktree，直接使用项目目录作为 Codex 工作目录；Diff 页面显示不可用状态。
- 合并必须由用户配置的流程节点控制；卡片完成不自动合并到基础分支。
- 合并节点目标固定为卡片 `baseBranch`；策略支持 `merge` 或 `rebase`。
- 合并节点执行时必须记录策略、base 分支、worktree 分支、base/head/merge commit 或合并失败错误码/原因。
- 合并冲突或失败时可以通过 `answer_user` 询问用户解决方案，并把回答作为当前节点后续处理或条件分支输入。
- 卡片完成、失败、停止、阻塞、待回答、恢复失败时都不自动清理 worktree。
- 只有用户手动关闭卡片或合并节点正常关闭卡片时，才允许清理 AnyCode 创建的 worktree；关闭前必须停止仍在运行的进程并释放等待中的 `answer_user`。
- `closed` 是终态，不能重新打开；关闭原因必须区分 `user_closed` 和 `merged_closed`；附件、事件、追加描述和合并记录都保留。
- 删除项目或清理会话时，只允许删除 AnyCode 创建的 worktree，永不删除用户原始项目目录。
- worktree 创建失败时必须回滚 session 创建或标记 failed，并清理本次创建的空目录和未归档附件。

## Codex CLI 兼容

- 服务启动时检查 `CODEX_BIN` 是否可执行，并记录版本和基础能力。
- 模型、思考强度或权限参数被当前 Codex CLI 拒绝时，会话必须进入 failed 并显示明确错误，不能静默降级为默认参数。
- Codex 命令行参数只在 `internal/infra/codexcli` 中拼装，领域层只保存配置值。
- 新建卡片的模型、思考强度和权限默认复用同项目上一张卡片；如果没有历史卡片，则不强制指定，让 Codex CLI 使用自身默认选择。

## 前端规范

- 第一屏是实际工作台，不做 landing page。
- 左侧是总揽按钮和项目列表；中间是会话卡片列表。
- 点击总揽显示跨项目卡片列表，不能按项目分组。
- 项目条目右侧有设置图标，点击后弹出菜单，菜单项依次包含“设置”“流程配置”和既有“移除项目”；“设置”打开项目设置窗口。
- 项目设置窗口第一版提供“工作树初始化命令”多行文本域，用于配置卡片创建 worktree 后执行的 shell 脚本。
- 卡片分为最近和历史；最近定义为最近 3 天内运行过。
- 历史区显示近 7 天运行记录，标题后有“更多”入口进入后端分页过滤的会话表格。
- 卡片必须清晰显示用户需求摘要、运行中/已停止、当前流程节点、待回答状态，并提供运行、恢复、停止、关闭动作。
- 会话详情页左侧主体显示会话事件流，包括思考内容、工具调用、模型输出、待回答和状态事件；底部是追加描述输入框，复用新建卡片的提示词输入区域，但去掉创建卡片说明文字，按钮按当前状态显示停止或发送图标。
- 会话详情页右侧使用 tab 面板：会话信息放在右侧 tab，模式模板只是会话信息里的小说明；当前分支变更 tab 显示文件列表，点击文件弹窗显示单文件 diff，并提供“查看全部”跳转完整 Diff 页面。
- 会话详情页必须提供完整 Diff 入口，打开类似 GitHub PR diff 的页面查看该卡片 worktree 变更；Diff 页必须支持“单个文件”和“全部 Diff”展示类型，diff 数据由后端计算。
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
- 应用数据目录通过 `ANYCODE_DATA_DIR` 配置；本地直接运行默认 `./data`，Docker Compose 默认 `/data`。
- 数据目录下按职责存放 `attachments/`、`logs/`、`worktrees/`；`codex/` 仅在需要隔离 Codex 配置、临时 MCP 配置或运行期辅助文件时使用。
- HTTP/GraphQL 使用 `Authorization: Bearer <ANYCODE_ACCESS_KEY>`；WebSocket 使用 `connection_init` payload；MCP endpoint 使用后端注入的内部凭据。
- 日志、事件 payload 和错误响应保留原始内容，不设置统一脱敏层；鉴权凭据由调用边界控制，不作为业务 payload 主动写入。
- 后端错误必须结构化返回 `code/category/message/details/retryable/userAction`；前端根据错误码和附加内容处理，不解析日志字符串。
- 事件、问题批次、追加描述、合并记录默认长期保留，用于之后查看和重造无法恢复的 Codex 会话。

## Diff 规则

- 有 worktree 的卡片按请求从 session worktree 实时计算 Diff；已执行合并节点的卡片也可从合并记录或 commit range 计算。
- 单个文件模式只返回当前文件 hunks；全部 Diff 模式由后端按文件顺序返回。
- 前端不能拉取全部 diff 后自行做分页、过滤或文件选择。

## Docker 规则

- Compose 默认提供 `./workspaces:/workspaces` 和 `./data:/data` 挂载，容器内 `ANYCODE_DATA_DIR=/data`。
- Compose 默认导出 `8080:8080`；Web UI、GraphQL HTTP、GraphQL WebSocket、附件预览/下载共用同一个 HTTP 服务端口。
- 不在代码中把目录选择限制为 `/workspaces`。
- 部署文档必须明确：持有访问密钥的人既可浏览后端进程可读路径，也可配置工作树初始化脚本，以 AnyCode 服务账号权限执行任意 shell；访问密钥等同该服务账号权限边界内的代码执行凭据。

## 简单优先

- 不为尚未需要的部署模式、权限模型、流程脚本语言或插件系统提前抽象。
- 优先使用标准库、项目已有依赖和明确领域端口。
- 不新增只透传数据的 wrapper、adapter 或重复 mapping。
- 如果实现开始依赖大量胶水代码，先回到领域边界检查数据形状和职责归属。
