# AnyCode

[中文](README.md) | [English](README.en.md)

面向 Codex agent 的 Web 工作台：在浏览器中统一管理项目、会话卡片、隔离 worktree、可编排工作流和人工介入。

![AnyCode 工作台预览](docs/assets/anycode-dashboard.png)

[核心能力](#核心能力) · [快速开始](#快速开始) · [首次使用](#首次使用) · [配置](#配置) · [数据与安全](#数据与安全) · [运维与排错](#运维与排错)

## 核心能力

- 在一个工作台中管理多个项目和会话，集中查看运行状态、优先级、TODO 与代码变更。
- Git 项目的会话可基于指定分支创建独立 worktree，减少并行任务互相干扰。
- 同时支持直接与 Codex 交互的会话模式，以及包含条件、重试、审批和合并节点的流程模式。
- Codex 可以通过 App Server 动态工具 `questions` 请求用户补充决策；工作流也可以暂停并等待人工审批。
- 会话时间线实时展示文本、推理、命令、工具调用和文件变更，执行完成后仍可回看。

## 主要功能

### 项目与会话

- 从服务端可访问的目录添加项目，并检测 Git 仓库与分支信息。
- 通过卡片查看会话状态、基础分支、工作分支、当前流程节点、优先级和最近活动。
- 配置 Codex 模型、推理强度和文件系统权限；追加需求时可以继续已有 Codex 会话。
- 通过全局并发上限和会话优先级控制排队执行。

### 工作流与人工介入

- 为项目配置由 Codex、表达式、人工审批、合并和关闭节点组成的流程。
- 按节点结果执行条件分支与重试，并在需要时暂停等待用户操作。
- 在界面中集中回答 Codex 提出的问题，或结合当前 Diff 审核流程结果。

### Git、文件与产物

- 查看单文件或全部 Diff、展开上下文，并查看会话提交历史。
- 上传附件作为 Codex 输入，在后续追加需求中继续使用。
- 归档产物并预览受支持的图片、PDF、音视频和文本；其他文件可下载，也可引用到当前追加提示。
- 卡片关闭时会清理可变输出和归档产物并保留已删除审计记录；提示中的产物引用不复制文件。
- 卡片关闭后，AnyCode 会异步清理已确认由自身创建并登记的 worktree；可重试的清理失败可从会话详情重新触发。

## 快速开始

### 前置条件

- Docker Engine
- Docker Compose 插件（使用 `docker compose` 命令）
- 可用于 Codex 的 ChatGPT 账号或 OpenAI API key

以下命令均在 AnyCode 仓库根目录执行。

### 1. 配置环境

复制示例配置：

```bash
cp .env.example .env
```

编辑 `.env`，至少把下面的默认值替换为足够长且随机的访问密钥：

```dotenv
ANYCODE_ACCESS_KEY=replace-with-a-long-random-secret
```

不要原样使用示例值，也不要提交生成后的 `.env`。

默认配置把 `./data` 挂载为容器用户目录，把 `./workspaces` 挂载到容器的 `/workspaces`：

```bash
mkdir -p data workspaces
```

需要由 AnyCode 管理的项目应位于 `ANYCODE_WORKSPACES_DIR` 指向的宿主机目录中。进入界面后，应使用对应的容器路径（默认是 `/workspaces/<项目目录>`）添加项目，而不是宿主机绝对路径。

### 2. 构建镜像并登录 Codex

```bash
docker compose build
docker compose run --rm anycode codex login --device-auth
docker compose run --rm anycode codex login status
```

也可以通过标准输入配置 OpenAI API key：

```bash
printf '%s' "$OPENAI_API_KEY" | docker compose run --rm -T anycode codex login --with-api-key
docker compose run --rm anycode codex login status
```

Codex 凭据写入容器的 `/home/anycode/.codex`，并由 `ANYCODE_HOST_DATA_DIR` 对应的宿主机目录持久化。不要提交或共享该目录。

### 3. 启动服务

```bash
docker compose up -d
docker compose ps
```

使用默认端口时，健康检查地址为：

```bash
curl --fail http://127.0.0.1:8080/healthz
```

然后打开 `http://127.0.0.1:8080`，输入 `.env` 中的 `ANYCODE_ACCESS_KEY`。

`ANYCODE_HTTP_PORT` 会同时改变服务监听端口、宿主机映射端口和镜像内置的 Docker healthcheck 地址。

## 首次使用

1. 首次打开且没有项目时，在自动弹出的目录选择窗口中选择 `/workspaces` 下的项目目录。
2. 如需执行依赖安装等准备工作，为项目配置 worktree 初始化命令；如需流程模式，先配置并启用项目工作流。
3. 返回概览页创建卡片，选择项目、基础分支、会话或流程模式，以及 Codex 模型、推理强度和权限。
4. 输入需求并附加必要文件。启动后可从卡片查看实时状态、TODO、待回答问题、待审批结果和 Diff。
5. 进入会话详情查看完整时间线、追加需求、管理产物或停止/关闭会话。

## 配置

部署配置以 [`.env.example`](.env.example) 和 [`compose.yml`](compose.yml) 为准。常用变量如下：

| 变量 | 默认值 | 用途 |
| --- | --- | --- |
| `ANYCODE_ACCESS_KEY` | 无可用默认值 | 访问 Web 数据、GraphQL、WebSocket 和会话文件的密钥；Compose 要求显式设置。 |
| `ANYCODE_HTTP_PORT` | `8080` | 宿主机发布端口和容器监听端口。 |
| `ANYCODE_HOST_DATA_DIR` | `./data` | 挂载到容器 `/home/anycode` 的宿主机目录，用于持久化 AnyCode 数据和 Codex 凭据。 |
| `ANYCODE_DATA_DIR` | `/home/anycode/.anycode` | 容器内数据库、附件、产物以及 AnyCode 创建的 worktree 目录。通常无需修改。 |
| `ANYCODE_WORKSPACES_DIR` | `./workspaces` | 挂载到容器 `/workspaces` 的宿主机项目目录。 |
| `ANYCODE_AGENT_MAX_CONCURRENT` | `1` | 同时运行的 Codex agent 上限；超出上限的会话进入队列。 |
| `TURSO_DATABASE_URL` | `/home/anycode/.anycode/anycode.turso.db` | 本地 Turso/libSQL 数据库路径，也可改为 `libsql://` 云数据库地址。 |
| `TURSO_AUTH_TOKEN` | 空 | 使用远程 Turso 数据库时的认证 token。 |

Turso 缓存与镜像构建代理变量可在 [`.env.example`](.env.example) 中查看；产物大小限制、Playwright 和 Chromium 配置可在 [`compose.yml`](compose.yml) 中查看。

## 数据与安全

- `ANYCODE_ACCESS_KEY` 是高权限访问凭据。持有者可以读取服务进程有权访问的目录，并可通过项目 worktree 初始化命令执行 shell。请使用随机密钥并只交给受信用户。
- 登录后，访问密钥会保存在浏览器 `localStorage` 中。只使用受信浏览器；使用共享设备后应在界面中退出登录，并在必要时清除该站点数据。
- 访问密钥只提供应用鉴权，不提供传输加密。Compose 的端口映射通常会监听宿主机所有网络接口；不要把服务直接暴露到不受信网络。远程访问时应自行配置防火墙和带 TLS 的可信反向代理或私有网络。
- 只把需要 AnyCode 访问的项目放入 `ANYCODE_WORKSPACES_DIR`。该挂载默认可读写，项目内命令和 Codex 进程拥有容器用户权限范围内的文件访问能力。
- 运行容器默认使用 UID/GID `1000`。workspace 必须对该用户可读写；入口脚本会调整 `/home/anycode` 挂载内容的所有权，因此不要把无关目录用作 `ANYCODE_HOST_DATA_DIR`。
- 本地数据库、附件、会话产物、活动 worktree 和 Codex 凭据都保存在 `ANYCODE_HOST_DATA_DIR` 下。备份时应保护整个目录，删除该目录会丢失持久化状态。
- Compose 为容器内浏览器能力增加了 `SYS_ADMIN` capability，并放宽 AppArmor/seccomp。只在受信宿主机上运行，并审查挂载范围。
- 不要手动删除、移动或重建 AnyCode 创建的会话 worktree；关闭卡片会按已记录的所有权提交异步清理，失败时应从会话详情使用重试操作。

## 运维与排错

查看状态和日志：

```bash
docker compose ps
docker compose logs -f anycode
```

重新构建并启动当前代码：

```bash
docker compose up -d --build
```

停止服务：

```bash
docker compose down
```

`docker compose down` 不会删除默认的宿主机 bind mount 数据。仍应在升级或迁移前备份 `ANYCODE_HOST_DATA_DIR`。

常见问题：

- Compose 提示必须设置 `ANYCODE_ACCESS_KEY`：确认仓库根目录存在 `.env`，且该值不是空字符串。
- 页面提示访问密钥无效：输入 `.env` 中的原始值，不要添加 `Bearer ` 前缀。
- 服务启动失败并出现 `probe codex cli`：先运行 `docker compose run --rm anycode codex login status` 检查凭据；凭据有效时，查看容器日志并重新构建镜像，确认镜像内 Codex CLI 完整且版本兼容。
- 会话执行时返回 Codex 认证错误：重新执行“构建镜像并登录 Codex”中的任一登录命令，再确认 `codex login status`。
- 无法添加或写入项目：确认宿主机目录已挂载到 `/workspaces`，并允许 UID/GID `1000` 读写。
- 本地数据库或附件写入失败：确认 `ANYCODE_HOST_DATA_DIR` 有可用空间且允许容器用户写入。
- 使用远程 Turso 失败：同时核对 `TURSO_DATABASE_URL`、`TURSO_AUTH_TOKEN` 和容器网络连接。
- 镜像构建下载失败：根据网络环境在 `.env` 中配置 `PACMAN_MIRROR`、`NPM_MIRROR` 或 `GOPROXY` 后重新构建。

## 许可证

AnyCode 使用 [MIT License](LICENSE)。
