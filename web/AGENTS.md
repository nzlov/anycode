# AGENTS.md

## 条目目录

- [适用范围](#适用范围)
- [下级规则索引](#下级规则索引)
- [界面约束](#界面约束)
- [常用入口](#常用入口)
- [验证](#验证)

## 适用范围

适用于 `web/` 及其子目录，继承[项目根规则](../AGENTS.md)。查询、订阅与状态更新遵循根规则的状态同步边界。

## 下级规则索引

- [服务调用](src/services/AGENTS.md)：适用于 `src/services/`；修改请求、订阅、时间线转换或类型声明时读取。
- [组合式逻辑](src/composables/AGENTS.md)：适用于 `src/composables/`；修改请求竞态、订阅生命周期或事件交互时读取。
- [主题与背景](src/theme/AGENTS.md)：适用于 `src/theme/`；修改主题模式、调色板或背景加载时读取。
- [PWA](src-pwa/AGENTS.md)：适用于 `src-pwa/`；修改 Service Worker、导航回退或推送通知时读取。
- [前端测试](tests/AGENTS.md)：适用于 `tests/`；新增或修改 Node 测试时读取。

## 界面约束

- 优先使用现有 Quasar 组件和统一图标集，遵循已有页面与响应式布局模式。
- 操作界面保持紧凑、可扫描；交互改动必须覆盖 hover、focus 和移动端溢出检查。

## 常用入口

- [src/pages/](src/pages/)、[src/layouts/](src/layouts/) 与 [src/components/](src/components/)：页面、布局及组件。
- [src/services/](src/services/)：GraphQL 调用、订阅传输及相关业务交互；优先沿用现有服务入口。
- [src/css/](src/css/) 与 [src/theme/](src/theme/)：样式和主题；图标配置见 [quasar.config.ts](quasar.config.ts)。
- [src-pwa/](src-pwa/)：PWA 注册、Service Worker 和 manifest。
- [tests/](tests/)：Node 测试；[package.json](package.json) 定义构建和类型检查命令。
- `npm --prefix web run build` 将 PWA 产物输出到 `internal/interfaces/http/static/pwa/`，由 Go 嵌入；通过前端源码和构建配置修改产物。

## 验证

以下命令在仓库根运行：

- 安装依赖：`npm --prefix web ci`；Node 版本要求见 `package.json` 的 `engines`。
- 前端测试：`node --test web/tests/*.test.mjs`，也可指定与改动相关的测试文件。
- 类型检查：`npm --prefix web run typecheck`。
- 生产构建：`npm --prefix web run build`。
- 开发预览：`npm --prefix web run dev`；涉及后端时按根规则启动隔离的测试实例。
- 界面改动实际查看运行效果，覆盖 hover、focus 和移动端溢出；常规收口执行根规则中的 `make verify`。
- `npm --prefix web run lint` 会写入格式化及自动修复结果，使用时检查并避免引入无关差异。
