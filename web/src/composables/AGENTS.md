# AGENTS.md

## 条目目录

- [适用范围](#适用范围)
- [请求与生命周期](#请求与生命周期)
- [事件展示交互](#事件展示交互)
- [验证](#验证)

## 适用范围

适用于 `web/src/composables/`，继承[前端规则](../../AGENTS.md)。

## 请求与生命周期

- [useSessionUpdates.ts](useSessionUpdates.ts) 和 [useSessionDetail.ts](useSessionDetail.ts) 使用 generation 或请求版本识别过期回调；停止、切换卡片或重新订阅后旧响应不能写入当前对象。
- 保持 start/stop 的幂等性，停止时注销订阅并清除重连定时器；鉴权失败与网络断开按现有策略区分，避免无限重连。
- composable 暴露显式 stop 时，由使用它的组件负责生命周期调用；修改时连同调用点检查清理行为。

## 事件展示交互

- [useDeferredTranscriptEvent.ts](useDeferredTranscriptEvent.ts) 通过注入的 loader 加载详情，保留当前展示条目的 ID 与 sourceEventIds；不能把旧事件详情显示到新事件上。
- [useEventStreamScroll.ts](useEventStreamScroll.ts) 保留用户向上查看历史时的位置；仅在跟随最新消息且未暂停时自动滚动，强制滚动需由明确交互触发。

## 验证

在仓库根按改动运行 `node --test web/tests/sessionDetailEventStream.test.mjs web/tests/sessionDeferredContent.test.mjs web/tests/eventStreamScroll.test.mjs`，并实际检查切换卡片、卸载重进与滚动位置。
