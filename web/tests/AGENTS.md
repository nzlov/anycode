# AGENTS.md

## 条目目录

- [适用范围](#适用范围)
- [测试组织](#测试组织)
- [运行入口](#运行入口)

## 适用范围

适用于 `web/tests/`，继承[前端规则](../AGENTS.md)。

## 测试组织

- 使用 Node 内置 `node:test` 和 `node:assert/strict`，测试命名为 `*.test.mjs`；统一验证按该命名发现本目录测试。
- 纯逻辑优先直接导入模块验证行为；涉及 Vue/TypeScript 的现有测试使用类型剥离、转译或 VM 执行，沿用相关文件已有方式，不另加测试框架。
- 部分测试导入 TypeScript、Vue 或业务依赖，运行前需按前端规则安装依赖；模块缺失不能当作功能断言失败。
- 源码合同断言只用于需要固定的接口或结构；行为变化优先覆盖输入、输出和边界，浏览器布局、hover、focus 和溢出仍需实际查看。
- 外部请求使用模拟或本地测试资源；需要后端实例时遵循根规则的隔离要求。

## 运行入口

- 在仓库根运行单文件：`node --test web/tests/graphqlSubscriptionTransport.test.mjs`。
- 前端全量测试：`node --test web/tests/*.test.mjs`。
- 统一验证还会执行类型检查和生产构建，Node 测试通过不代表这两项已通过。
