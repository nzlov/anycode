# AGENTS.md

## 条目目录

- [适用范围](#适用范围)
- [路由与鉴权](#路由与鉴权)
- [静态资源](#静态资源)
- [验证](#验证)

## 适用范围

适用于 `internal/interfaces/http/`，继承[后端规则](../../AGENTS.md)。

## 路由与鉴权

- [server.go](server.go) 注册 HTTP 与 GraphQL 路由；新增受保护路由沿用对应鉴权入口，WebSocket 需保留连接初始化鉴权。
- [file_access_token.go](file_access_token.go) 为预览、下载生成短期令牌；令牌绑定具体资源和访问模式，不能跨文件、卡片、路径或预览/下载模式复用。
- token 请求保持私有且不缓存的响应语义；文件读取仍由应用用例解析和校验资源，不能把 URL 参数直接当可信磁盘路径。
- [terminal_websocket.go](terminal_websocket.go) 管理终端连接；修改时同时检查认证、卡片绑定与连接关闭。

## 静态资源

- [static/static.go](static/static.go) 嵌入 `static/pwa/`；正式前端改动在 `web/` 源码完成，再通过前端构建生成产物。
- HTTP 静态回退与 PWA Service Worker 都可能影响文件预览；新增文件或 API 路由时同时检查 `web/src-pwa/` 的导航排除规则。

## 验证

在仓库根运行 `go test ./internal/interfaces/http`。令牌变更覆盖 [file_access_token_test.go](file_access_token_test.go) 的过期、伪造、资源绑定与 Range 场景；集成测试需要本地监听权限，启动实例仍遵循根规则的数据库隔离要求。
