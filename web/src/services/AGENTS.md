# AGENTS.md

## 条目目录

- [适用范围](#适用范围)
- [请求与订阅](#请求与订阅)
- [时间线与类型](#时间线与类型)
- [验证](#验证)

## 适用范围

适用于 `web/src/services/`，继承[前端规则](../../AGENTS.md)。

## 请求与订阅

- [graphqlClient.ts](graphqlClient.ts) 统一 GraphQL 鉴权、结构化错误和通知；访问密钥变化时重置订阅传输，不在各业务服务另建鉴权逻辑。
- [graphqlSubscriptionTransport.js](graphqlSubscriptionTransport.js) 管理共享传输及操作订阅；保留 unsubscribe、连接确认和服务端完成的区别。
- 业务服务按已有 schema 发起查询与 mutation，订阅合同遵循项目根规则；重新连接后的资源刷新由调用方协调，不在传输层存储业务对象。

## 时间线与类型

- [sessionTimelineReducer.js](sessionTimelineReducer.js) 按顺序与 correlation ID 合并转录事件；保留源事件 ID、延迟加载引用及命令结果关联。
- 历史分页、实时事件去重和详情读取分别使用现有服务入口；避免重复请求覆盖已加载的新内容。
- 本目录的 `.js` 模块若有配套 `.d.ts`，修改导出签名或数据结构时同步声明，保证 Node 测试与 TypeScript 调用方一致。

## 验证

在仓库根按改动运行相关测试，例如 `node --test web/tests/graphqlSubscriptionTransport.test.mjs web/tests/sessionTimelineReducer.test.mjs`；类型合同变化后运行 `npm --prefix web run typecheck`。
