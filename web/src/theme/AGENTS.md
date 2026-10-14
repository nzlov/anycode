# AGENTS.md

## 条目目录

- [适用范围](#适用范围)
- [主题与背景](#主题与背景)
- [验证](#验证)

## 适用范围

适用于 `web/src/theme/`，继承[前端规则](../../AGENTS.md)。

## 主题与背景

- [tokens.ts](tokens.ts) 管理 system/light/dark 模式并同步 Quasar `Dark`；[appearance.ts](appearance.ts) 应用背景配置，两者职责保持清楚。
- [dailyBackgroundModel.js](dailyBackgroundModel.js) 处理调色板和缓存记录解析，[dailyBackground.ts](dailyBackground.ts) 处理异步加载与页面应用；相关类型变化同步 `.d.ts`。
- 背景切换递增请求版本，过期加载结果不得覆盖新选择；上传图片的 object URL 在替换或离开对应来源时释放。
- 主题变化检查浅色、深色、背景遮罩和文字/表面颜色的对比度，沿用现有 CSS 变量与 Material 调色板。

## 验证

在仓库根运行 `node --test web/tests/themeSystem.test.mjs web/tests/themeContrast.test.mjs web/tests/dailyBackgroundTheme.test.mjs`；视觉修改实际检查模式切换及不同背景下的可读性。
