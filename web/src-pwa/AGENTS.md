# AGENTS.md

## 条目目录

- [适用范围](#适用范围)
- [导航与缓存](#导航与缓存)
- [推送通知](#推送通知)
- [验证](#验证)

## 适用范围

适用于 `web/src-pwa/`，继承[前端规则](../AGENTS.md)。

## 导航与缓存

- [sw/custom-sw.ts](sw/custom-sw.ts) 是自定义 Service Worker，入口配置在 `web/quasar.config.ts` 的 `sourceFiles`。
- 导航回退排除 `/files`、`/api` 和 `/tunnel/`，文件预览 iframe 也属于导航请求，必须到达后端鉴权与文件响应。
- 保留 Workbox 预缓存 manifest 和过期缓存清理；路由变化同时检查后端静态回退行为。

## 推送通知

- 推送数据校验标题、正文、卡片 ID、状态、路径和 tag；导航路径限定为应用卡片入口。
- 当前可见窗口已在目标卡片时避免重复通知；点击通知优先聚焦同源窗口并导航，无合适窗口时再打开。

## 验证

在仓库根运行 `node --test web/tests/serviceWorkerNavigation.test.mjs web/tests/pushNotifications.test.mjs`；执行 `npm --prefix web run build` 检查 PWA 构建，缓存或导航行为变化还需在浏览器中确认。
