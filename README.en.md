# AnyCode

[中文](README.md) | [English](README.en.md)

A web workspace for Codex agents that lets you manage projects, session cards, isolated worktrees, orchestrated workflows, and human intervention from your browser.

![AnyCode dashboard preview](docs/assets/anycode-dashboard.png)

[Key capabilities](#key-capabilities) · [Quick start](#quick-start) · [First use](#first-use) · [Configuration](#configuration) · [Data and security](#data-and-security) · [Operations and troubleshooting](#operations-and-troubleshooting)

## Key capabilities

- Manage multiple projects and sessions in one workspace, with a consolidated view of runtime status, priority, TODOs, and code changes.
- Create an isolated worktree from a selected branch for each Git project session, reducing interference between concurrent tasks.
- Use either a session mode for direct interaction with Codex or a workflow mode with conditions, retries, approvals, and merge nodes.
- Let Codex request additional decisions through `answer_user`, while workflows can also pause for manual approval.
- Follow text, reasoning, commands, tool calls, and file changes in real time on the session timeline, and review them after execution completes.

## Features

### Projects and sessions

- Add projects from directories accessible to the server and detect their Git repositories and branches.
- View session status, base and working branches, the current workflow node, priority, and recent activity on each card.
- Configure the Codex model, reasoning effort, and filesystem permissions, and continue an existing Codex session with follow-up requests.
- Control queued execution through a global concurrency limit and per-session priority.

### Workflows and human intervention

- Configure project workflows composed of Codex, expression, manual approval, merge, and close nodes.
- Apply conditional branches and retries based on node results, pausing for user action when required.
- Answer Codex questions in one place or review workflow results alongside the current diff.

### Git, files, and artifacts

- Inspect individual or aggregate diffs, expand surrounding context, and review session commit history.
- Upload attachments as Codex input and reuse them in later follow-up requests.
- Archive artifacts and preview supported images, PDFs, audio, video, and text. Other files can be downloaded or referenced in the current follow-up prompt.
- Closing a card removes mutable output and archived artifacts while retaining deletion audit records. Artifact references in prompts do not copy files.
- After a card is closed, AnyCode asynchronously cleans up worktrees that it has confirmed it created and registered. Retriable cleanup failures can be triggered again from the session details.

## Quick start

### Prerequisites

- Docker Engine
- Docker Compose plugin (using the `docker compose` command)
- A ChatGPT account or OpenAI API key that can be used with Codex

Run the following commands from the AnyCode repository root.

### 1. Configure the environment

Copy the example configuration:

```bash
cp .env.example .env
```

Edit `.env` and, at minimum, replace the value below with a sufficiently long, random access key:

```dotenv
ANYCODE_ACCESS_KEY=replace-with-a-long-random-secret
```

Do not use the example value as-is, and do not commit the generated `.env` file.

By default, `./data` is mounted as the container user's home directory and `./workspaces` is mounted at `/workspaces` inside the container:

```bash
mkdir -p data workspaces
```

Projects managed by AnyCode should be located in the host directory specified by `ANYCODE_WORKSPACES_DIR`. In the UI, add a project using its corresponding container path (by default, `/workspaces/<project-directory>`) rather than its absolute host path.

### 2. Build the image and sign in to Codex

```bash
docker compose build
docker compose run --rm anycode codex login --device-auth
docker compose run --rm anycode codex login status
```

You can also provide an OpenAI API key through standard input:

```bash
printf '%s' "$OPENAI_API_KEY" | docker compose run --rm -T anycode codex login --with-api-key
docker compose run --rm anycode codex login status
```

Codex credentials are written to `/home/anycode/.codex` in the container and persisted in the host directory configured by `ANYCODE_HOST_DATA_DIR`. Do not commit or share this directory.

### 3. Start the service

```bash
docker compose up -d
docker compose ps
```

With the default port, the health-check endpoint is:

```bash
curl --fail http://127.0.0.1:8080/healthz
```

Then open `http://127.0.0.1:8080` and enter the `ANYCODE_ACCESS_KEY` value from `.env`.

Keeping the default port `8080` is recommended. `ANYCODE_HTTP_PORT` changes both the service's listening port and the published host port, but the Docker health check built into the current image always accesses container port `8080`. If you change the port, also override the health check in a custom Compose configuration; otherwise, the service may be accessible while the container is marked unhealthy.

## First use

1. When you first open AnyCode with no projects configured, select a project directory under `/workspaces` in the directory picker that appears automatically.
2. If dependency installation or other preparation is required, configure a worktree initialization command for the project. To use workflow mode, configure and enable a project workflow first.
3. Return to the overview and create a card. Select the project, base branch, session or workflow mode, Codex model, reasoning effort, and permissions.
4. Enter your request and attach any required files. Once execution starts, use the card to follow live status, TODOs, pending questions, pending approvals, and diffs.
5. Open the session details to inspect the full timeline, send follow-up requests, manage artifacts, or stop and close the session.

## Configuration

Use [`.env.example`](.env.example) and [`compose.yml`](compose.yml) as the source of truth for deployment configuration. Common variables are listed below:

| Variable | Default | Purpose |
| --- | --- | --- |
| `ANYCODE_ACCESS_KEY` | No usable default | Access key for web data, GraphQL, WebSocket, MCP, and session files. Compose requires an explicit value. |
| `ANYCODE_HTTP_PORT` | `8080` | Published host port and service listening port. |
| `ANYCODE_HOST_DATA_DIR` | `./data` | Host directory mounted at `/home/anycode` in the container to persist AnyCode data and Codex credentials. |
| `ANYCODE_DATA_DIR` | `/home/anycode/.anycode` | Container directory for the database, attachments, artifacts, and worktrees created by AnyCode. This usually does not need to be changed. |
| `ANYCODE_WORKSPACES_DIR` | `./workspaces` | Host project directory mounted at `/workspaces` in the container. |
| `ANYCODE_AGENT_MAX_CONCURRENT` | `1` | Maximum number of Codex agents that may run concurrently. Additional sessions are queued. |
| `TURSO_DATABASE_URL` | `/home/anycode/.anycode/anycode.turso.db` | Local Turso/libSQL database path. This can also be a `libsql://` cloud database URL. |
| `TURSO_AUTH_TOKEN` | Empty | Authentication token for a remote Turso database. |

See [`.env.example`](.env.example) for Turso caching and image-build proxy variables. See [`compose.yml`](compose.yml) for artifact size limits and Playwright and Chromium configuration.

## Data and security

- `ANYCODE_ACCESS_KEY` is a high-privilege credential. Anyone who has it can read directories available to the service process and execute shell commands through project worktree initialization commands. Use a random key and share it only with trusted users.
- After login, the access key is stored in the browser's `localStorage`. Use only trusted browsers. Sign out after using a shared device and clear the site's data when necessary.
- The access key provides application authentication, not transport encryption. Compose port mappings generally listen on all host network interfaces. Do not expose the service directly to untrusted networks. For remote access, configure a firewall and a trusted TLS reverse proxy or private network.
- Only place projects that AnyCode needs to access in `ANYCODE_WORKSPACES_DIR`. This mount is read-write by default, and project commands and Codex processes can access files within the container user's permission scope.
- The container runs with UID/GID `1000` by default. Workspaces must be readable and writable by this user. The entrypoint adjusts ownership of the `/home/anycode` mount, so do not use an unrelated directory as `ANYCODE_HOST_DATA_DIR`.
- The local database, attachments, session artifacts, active worktrees, and Codex credentials are all stored under `ANYCODE_HOST_DATA_DIR`. Protect the entire directory when backing it up; deleting it removes all persisted state.
- Compose adds the `SYS_ADMIN` capability and relaxes AppArmor/seccomp restrictions for browser functionality inside the container. Run it only on trusted hosts and review the mounted paths.
- Do not manually delete, move, or recreate session worktrees created by AnyCode. Closing a card schedules asynchronous cleanup based on recorded ownership, and failed cleanup can be retried from the session details.

## Operations and troubleshooting

View status and logs:

```bash
docker compose ps
docker compose logs -f anycode
```

Rebuild and start the current code:

```bash
docker compose up -d --build
```

Stop the service:

```bash
docker compose down
```

`docker compose down` does not remove the default host bind-mount data. You should still back up `ANYCODE_HOST_DATA_DIR` before upgrading or migrating.

Common issues:

- Compose reports that `ANYCODE_ACCESS_KEY` must be set: confirm that `.env` exists in the repository root and that its value is not empty.
- The page reports an invalid access key: enter the raw value from `.env` without a `Bearer ` prefix.
- The service fails to start with `probe codex cli`: run `docker compose run --rm anycode codex login status` to check the credentials. If they are valid, inspect the container logs and rebuild the image to ensure the bundled Codex CLI is complete and compatible.
- A session returns a Codex authentication error: repeat one of the login commands under "Build the image and sign in to Codex," then confirm the result with `codex login status`.
- A project cannot be added or written: confirm that the host directory is mounted at `/workspaces` and is writable by UID/GID `1000`.
- The local database or attachments cannot be written: confirm that `ANYCODE_HOST_DATA_DIR` has available space and is writable by the container user.
- A remote Turso connection fails: verify `TURSO_DATABASE_URL`, `TURSO_AUTH_TOKEN`, and the container's network connectivity.
- Image-build downloads fail: configure `PACMAN_MIRROR`, `NPM_MIRROR`, or `GOPROXY` in `.env` for your network environment, then rebuild.

## License

AnyCode is licensed under the [MIT License](LICENSE).
