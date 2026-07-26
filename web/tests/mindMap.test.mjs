import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

test('global settings expose realtime and async mind map configuration', () => {
  const dialog = readSource('../src/components/GlobalSettingsDialog.vue');
  const service = readSource('../src/services/generalSettings.ts');

  for (const field of [
    'mindMapEnabled',
    'mindMapMode',
    'mindMapModel',
    'mindMapReasoningEffort',
    'mindMapMaxConcurrent',
  ]) {
    assert.match(service, new RegExp(field));
  }
  assert.match(dialog, /aria-label="项目思维图"/);
  assert.match(dialog, /v-model="general\.mindMapMode"/);
  assert.match(dialog, /label: '实时', value: 'realtime'/);
  assert.match(dialog, /label: '异步', value: 'async'/);
  assert.match(
    dialog,
    /v-if="general\.mindMapEnabled && general\.mindMapMode === 'async'"[\s\S]*<CodexModelSelector/,
  );
  assert.match(dialog, /label="全局并发任务数"/);
});

test('project mind map is gated and opened from the project popup menu', () => {
  const settings = readSource('../src/components/ProjectSettingsDialog.vue');
  const projects = readSource('../src/pages/ProjectsPage.vue');
  const routes = readSource('../src/router/routes.ts');

  assert.match(settings, /label="启用项目思维图"/);
  assert.match(settings, /mindMapAvailable/);
  assert.match(projects, /<q-item-section>思维图<\/q-item-section>/);
  assert.match(projects, /v-if="mindMapVisible\(project\)"/);
  assert.match(projects, /return Boolean\(generalSettings\.value\?\.mindMapEnabled && project\.mindMapEnabled\)/);
  assert.match(routes, /path: 'projects\/:projectId\/mind-map'/);
});

test('mind map editor keeps only the project-name root fixed and supports free form edits', () => {
  const page = readSource('../src/pages/ProjectMindMapPage.vue');
  const service = readSource('../src/services/mindMaps.ts');

  assert.match(page, /const rootNodeId = 'project-root'/);
  assert.match(page, /draggable: node\.id !== rootNodeId/);
  assert.match(page, /项目名根节点固定在中心，只能作为关系端点/);
  assert.match(page, /kind: 'upsert_node'/);
  assert.match(page, /kind: 'upsert_edge'/);
  assert.match(page, /kind: 'delete_node'/);
  assert.match(page, /kind: 'delete_edge'/);
  assert.doesNotMatch(page, /需求节点|功能节点|决策节点/);
  assert.match(service, /query ProjectMindMap/);
  assert.match(service, /mutation UpdateProjectMindMap/);
  assert.match(service, /mutation RetryMindMapTask/);
});

test('realtime cards have merge-close while async cards use ordinary close', () => {
  const index = readSource('../src/pages/IndexPage.vue');
  const menu = readSource('../src/components/SessionCardContextMenu.vue');
  const detail = readSource('../src/components/SessionDetailView.vue');
  const sessions = readSource('../src/services/sessions.ts');

  assert.match(index, /generalSettings\.value\.mindMapMode === 'realtime'/);
  assert.match(menu, /v-if="mindMapRealtime"[\s\S]*合并思维图并关闭/);
  assert.match(detail, /v-if="mindMapRealtime"[\s\S]*label="合并思维图并关闭"/);
  assert.match(sessions, /reason: 'user_closed' \| 'merged_closed'/);
});

test('GraphQL exposes project main and card graphs plus async task state', () => {
  const schema = readSource('../../internal/interfaces/graphql/graph/schema.graphqls');

  assert.match(schema, /projectMindMap\(projectId: ID!, sessionId: ID\): MindMapGraph!/);
  assert.match(schema, /projectMindMapCards\(projectId: ID!\): \[MindMapCard!/);
  assert.match(schema, /updateProjectMindMap\(input: UpdateMindMapInput!\): MindMapGraph!/);
  assert.match(schema, /retryMindMapTask\(id: ID!\): MindMapCard!/);
  assert.match(schema, /taskStatus: String!/);
  assert.match(schema, /taskError: String!/);
});
