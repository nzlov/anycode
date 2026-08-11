# 使用 systemd 用户服务部署 AnyCode

[中文](systemd.md) | [English](systemd.en.md)

GitHub Release 同时提供 Linux 二进制归档和 Debian、Red Hat、Arch Linux 安装包。它们都包含已嵌入 Web 前端的 `anycode` 可执行文件、systemd 用户 unit 和环境变量示例。

AnyCode 以当前登录用户运行，直接使用该用户的 `HOME`、`~/.codex` 凭据和项目访问权限，不创建额外的系统用户。宿主机仍需安装 Codex CLI；只有使用 Cloudflare 临时隧道时才需要 `cloudflared`。

## 支持的平台

- Linux x86-64：`amd64`
- Linux ARM64：`arm64`
- 使用 systemd 和 glibc 的发行版（发布包不支持 musl）

发行版包使用各自的架构名称：

| 平台   | Debian  | Red Hat   | Arch Linux |
| ------ | ------- | --------- | ---------- |
| x86-64 | `amd64` | `x86_64`  | `x86_64`   |
| ARM64  | `arm64` | `aarch64` | `aarch64`  |

Arch Linux ARM 的 `aarch64` 包与其他包一起生成；Arch Linux 官方仓库本身只支持 x86-64。

## 使用发行版安装包

下面以 `1.2.3` 和 x86-64 为例。选择与系统对应的一组命令，不要同时安装多种格式。

Debian 或 Ubuntu：

```bash
VERSION=1.2.3
ARCH=amd64
curl -fLO "https://github.com/nzlov/anycode/releases/download/v${VERSION}/anycode_${VERSION}_${ARCH}.deb"
curl -fLO "https://github.com/nzlov/anycode/releases/download/v${VERSION}/checksums.txt"
sha256sum --check --ignore-missing checksums.txt
sudo apt install "./anycode_${VERSION}_${ARCH}.deb"
```

RHEL、Fedora 或其他 RPM 系统：

```bash
VERSION=1.2.3
ARCH=x86_64
curl -fLO "https://github.com/nzlov/anycode/releases/download/v${VERSION}/anycode-${VERSION}-1.${ARCH}.rpm"
curl -fLO "https://github.com/nzlov/anycode/releases/download/v${VERSION}/checksums.txt"
sha256sum --check --ignore-missing checksums.txt
sudo dnf install "./anycode-${VERSION}-1.${ARCH}.rpm"
```

Arch Linux 或 Arch Linux ARM：

```bash
VERSION=1.2.3
ARCH=x86_64
curl -fLO "https://github.com/nzlov/anycode/releases/download/v${VERSION}/anycode-${VERSION}-1-${ARCH}.pkg.tar.zst"
curl -fLO "https://github.com/nzlov/anycode/releases/download/v${VERSION}/checksums.txt"
sha256sum --check --ignore-missing checksums.txt
sudo pacman -U "./anycode-${VERSION}-1-${ARCH}.pkg.tar.zst"
```

发行版包安装 Git 和 systemd 依赖，并由包管理器维护以下文件：

- `/usr/bin/anycode`
- `/usr/lib/systemd/user/anycode.service`
- `/usr/share/anycode/anycode.env.example`

Release 中的发行版包未使用发行版仓库密钥签名；请通过同一 Release 的 `checksums.txt` 校验下载文件。

为当前用户创建私有配置：

```bash
install -d -m 0700 ~/.config/anycode
install -m 0600 /usr/share/anycode/anycode.env.example ~/.config/anycode/anycode.env
```

包安装和升级不会覆盖 `~/.config/anycode/anycode.env`，也不会自动启动任何用户的服务。

## 使用二进制归档

下面以 `v1.2.3` 和 `amd64` 为例。请把版本与架构替换为目标 Release 和宿主机架构。

```bash
VERSION=v1.2.3
ARCH=amd64
curl -fLO "https://github.com/nzlov/anycode/releases/download/${VERSION}/anycode-${VERSION}-linux-${ARCH}.tar.gz"
curl -fLO "https://github.com/nzlov/anycode/releases/download/${VERSION}/checksums.txt"
sha256sum --check --ignore-missing checksums.txt
tar -xzf "anycode-${VERSION}-linux-${ARCH}.tar.gz"
cd "anycode-${VERSION}-linux-${ARCH}"
```

安装二进制，并为当前用户安装 unit 和配置：

```bash
sudo install -m 0755 anycode /usr/bin/anycode
install -D -m 0644 systemd/anycode.service ~/.config/systemd/user/anycode.service
install -D -m 0600 systemd/anycode.env.example ~/.config/anycode/anycode.env
```

## 配置 Codex 和 AnyCode

先确认当前用户已登录 Codex：

```bash
codex login --device-auth
codex login status
command -v codex
```

用户服务默认搜索 `~/.local/bin`、`/usr/local/bin` 和 `/usr/bin`。如果 `command -v codex` 返回其他目录，请把完整路径写入 `~/.config/anycode/anycode.env` 的 `CODEX_BIN`。

编辑 `~/.config/anycode/anycode.env`，至少替换 `ANYCODE_ACCESS_KEY`。环境文件不是 shell 脚本，应使用简单的 `KEY=value`，不要使用 `export`、命令替换或引用其他变量。

服务默认把数据库、附件、产物和 worktree 写入 `~/.local/share/anycode`。项目可放在当前用户有权访问的任意目录；在 Web 界面添加项目时使用宿主机真实路径。

默认仅监听 `127.0.0.1:8080`。需要远程访问时，建议保留回环监听并通过可信的 TLS 反向代理开放服务。AnyCode 会继承当前用户的文件权限，访问密钥具有读取这些目录和执行项目命令的高权限，不能替代传输加密。

## 启动与检查

以下 `systemctl --user` 命令必须由实际运行 AnyCode 的用户执行，不要使用 `sudo systemctl --user`。

服务器常驻部署需要在主机启动后、用户未登录时也运行服务。先为当前用户显式启用 lingering；如果只需在登录期间运行，可跳过此步骤：

```bash
sudo loginctl enable-linger "$USER"
loginctl show-user "$USER" -p Linger
```

确认输出包含 `Linger=yes`。发行版包不会自动为任何用户启用 lingering，因为这是影响该用户所有 systemd 用户服务的主机级策略。

然后加载 unit、启用并检查 AnyCode：

```bash
systemctl --user daemon-reload
systemctl --user enable --now anycode
systemctl --user status anycode
systemctl --user is-enabled anycode
curl --fail http://127.0.0.1:8080/healthz
```

查看日志：

```bash
journalctl --user -u anycode -f
```

如果启动日志包含 `probe codex cli`，检查 `CODEX_BIN` 和当前用户的 `codex login status`。

## 升级

升级前备份 `~/.local/share/anycode` 和 `~/.config/anycode`。

使用发行版包时，下载并校验新 Release，然后再次执行对应的 `apt install`、`dnf install` 或 `pacman -U` 命令。之后由运行服务的用户加载新 unit 并重启：

```bash
systemctl --user daemon-reload
systemctl --user restart anycode
systemctl --user status anycode
```

使用二进制归档时，先替换 `/usr/bin/anycode`；如果归档中的 unit 有变化，再复制到 `~/.config/systemd/user/anycode.service`，然后执行上面的用户服务命令。环境变量示例不会自动覆盖现有配置。

## 卸载

先由每个运行 AnyCode 的用户停止并禁用服务：

```bash
systemctl --user disable --now anycode
```

如果此前专门为 AnyCode 启用了 lingering，并确认该用户没有其他需要在未登录时运行的用户服务，再将其关闭：

```bash
sudo loginctl disable-linger "$USER"
```

Lingering 影响该用户的所有 systemd 用户服务；如果其他服务仍依赖它，不要因卸载 AnyCode 而关闭。

使用发行版包时，选择当前系统对应的一条命令：

```bash
sudo apt remove anycode
sudo dnf remove anycode
sudo pacman -R anycode
```

使用二进制归档时：

```bash
rm ~/.config/systemd/user/anycode.service
systemctl --user daemon-reload
sudo rm /usr/bin/anycode
```

卸载不会自动删除 `~/.config/anycode`、`~/.local/share/anycode` 或项目目录；确认不再需要后再单独处理。
