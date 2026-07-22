import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const layoutSource = readSource('../src/layouts/MainLayout.vue');
const toolbarSource = readSource('../src/components/PageToolbar.vue');
const indexSource = readSource('../src/pages/IndexPage.vue');
const sessionsSource = readSource('../src/pages/SessionsPage.vue');
const diffPageSource = readSource('../src/pages/DiffPage.vue');
const diffWorkspaceSource = readSource('../src/components/DiffWorkspace.vue');
const commitHistorySource = readSource('../src/pages/CommitHistoryPage.vue');
const workflowSource = readSource('../src/pages/WorkflowConfigPage.vue');
const stylesSource = readSource('../src/css/app.scss');

test('route pages render their titles and actions into the application toolbar', () => {
  assert.match(layoutSource, /id="app-page-toolbar"/);
  assert.match(toolbarSource, /<Teleport defer to="#app-page-toolbar">/);

  assert.match(indexSource, /<PageToolbar title="AnyCode"/);
  assert.match(
    indexSource,
    /<PageToolbar[\s\S]*class="overview-filter-toolbar"[\s\S]*<\/PageToolbar>/,
  );
  assert.match(sessionsSource, /<PageToolbar title="会话表格"/);
  assert.match(sessionsSource, /class="sessions-toolbar__search"/);
  assert.match(sessionsSource, /class="sessions-toolbar__status"/);
  assert.match(commitHistorySource, /<PageToolbar title="提交记录"/);
  assert.match(workflowSource, /<PageToolbar title="流程配置"/);

  for (const source of [sessionsSource, diffPageSource, commitHistorySource, workflowSource]) {
    assert.doesNotMatch(source, /class="page-heading"/);
  }
  assert.doesNotMatch(sessionsSource, /table-filter-card/);
});

test('overview navigation and title use the AnyCode project icon', () => {
  assert.match(layoutSource, /icon="img:\/icons\/anycode\.svg"/);
  assert.match(layoutSource, /aria-label="返回总览"/);
  assert.match(
    indexSource,
    /<PageToolbar title="AnyCode" title-icon="img:\/icons\/anycode\.svg"/,
  );
  assert.match(toolbarSource, /<q-icon v-if="titleIcon" :name="titleIcon"/);
  const titleIconStyles =
    stylesSource.match(/\.page-toolbar__title-icon\s*{([^}]*)}/)?.[1] ?? '';
  assert.match(titleIconStyles, /font-size:\s*28px/);
  assert.match(titleIconStyles, /margin-right:\s*8px/);
});

test('standalone diff keeps its state owner while moving controls into the header', () => {
  assert.match(diffPageSource, /toolbar-target="#app-page-toolbar"/);
  assert.match(diffPageSource, /toolbar-title="当前分支变更"/);
  assert.match(diffWorkspaceSource, /toolbarTarget\?: string/);
  assert.match(diffWorkspaceSource, /toolbarTitle\?: string/);
  assert.match(diffWorkspaceSource, /<Teleport defer[\s\S]*:disabled="!toolbarTarget"/);
  assert.match(diffWorkspaceSource, /diff-workspace__toolbar--header/);
  assert.match(diffWorkspaceSource, /\$q\.screen\.lt\.sm \? \{\} : \{ label: '单个文件' \}/);
  assert.match(diffWorkspaceSource, /'aria-label': '全部 Diff'/);
  assert.match(workflowSource, /:label="\$q\.screen\.lt\.sm \? undefined : '保存'"/);
});

test('page toolbar layout remains single-line and shrinkable on narrow screens', () => {
  assert.match(
    stylesSource,
    /\.app-page-toolbar-host\s*{[^}]*min-width:\s*0[^}]*flex:\s*1\s+1\s+auto/s,
  );
  assert.match(
    stylesSource,
    /\.page-toolbar__actions\s*{[^}]*min-width:\s*0[^}]*overflow:\s*hidden/s,
  );
  assert.match(
    stylesSource,
    /@media \(max-width:\s*599\.98px\)[\s\S]*?\.page-toolbar--compact-title \.page-toolbar__title\s*{[^}]*display:\s*none/s,
  );
  const projectFilterStyles =
    stylesSource.match(/\.overview-project-filters\s*{([^}]*)}/)?.[1] ?? '';
  assert.match(projectFilterStyles, /overflow-x:\s*auto/);
  assert.match(projectFilterStyles, /flex-wrap:\s*nowrap/);
});
