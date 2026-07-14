import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const sources = [
  '../src/pages/IndexPage.vue',
  '../src/pages/SessionsPage.vue',
  '../src/pages/DiffPage.vue',
  '../src/pages/CommitHistoryPage.vue',
  '../src/pages/WorkflowConfigPage.vue',
  '../src/pages/LoginPage.vue',
  '../src/components/NewSessionDialog.vue',
  '../src/components/ProjectDirectoryDialog.vue',
  '../src/components/ProjectSettingsDialog.vue',
  '../src/components/AnswerUserDialog.vue',
  '../src/layouts/MainLayout.vue',
].map(readSource);

test('pages and dialog headers do not render the removed subtitles', () => {
  const combinedSource = sources.join('\n');
  const removedSubtitles = [
    '配置项目、分支和 Codex 运行参数',
    '目录树由后端权限范围决定',
    '选择每个问题的答案后一起提交。',
    '分页、过滤、排序和 total 均由 GraphQL 后端计算',
    '最新卡片与历史卡片',
    '未关闭的卡片，按最近操作倒序',
    '已关闭的卡片，按最近操作倒序',
    '从左侧拖入节点类型',
    '输入访问密钥后进入工作台',
    '会停止该项目所有运行中的卡片，并从列表隐藏。',
    '退出后需要重新输入访问密钥。',
  ];

  for (const subtitle of removedSubtitles) {
    assert.doesNotMatch(combinedSource, new RegExp(subtitle));
  }

  assert.doesNotMatch(combinedSource, /project\?\.name/);
});
