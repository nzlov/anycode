import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

import { buildRadialLayout, radialEdgeHandles } from '../src/services/mindMapFlowModel.js';

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
  assert.match(
    projects,
    /return Boolean\(generalSettings\.value\?\.mindMapEnabled && project\.mindMapEnabled\)/,
  );
  assert.match(routes, /path: 'projects\/:projectId\/mind-map'/);
});

test('mind map uses a full-screen radial canvas with direct relation highlighting', () => {
  const page = readSource('../src/pages/ProjectMindMapPage.vue');
  const service = readSource('../src/services/mindMaps.ts');

  assert.match(page, /const rootNodeId = 'project-root'/);
  assert.match(page, /buildRadialLayout/);
  assert.match(page, /type: 'default'/);
  assert.match(page, /directlyRelatedNodeIds/);
  assert.match(page, /directlyRelatedEdgeIds/);
  assert.match(page, /mind-map-element--muted/);
  assert.match(page, /@nodes-initialized="fitGraph"/);
  assert.match(page, /<div class="mind-map-canvas">/);
  assert.match(page, /flex: 1 1 auto/);
  assert.match(
    page,
    /\.mind-map-canvas :deep\(\.vue-flow\)[\s\S]*position: absolute;[\s\S]*inset: 0/,
  );
  assert.doesNotMatch(page, /fit-view-on-init/);
  assert.doesNotMatch(page, /mind-map-editor/);
  assert.match(page, /kind: 'upsert_node'/);
  assert.match(page, /kind: 'upsert_edge'/);
  assert.doesNotMatch(page, /需求节点|功能节点|决策节点/);
  assert.match(service, /query ProjectMindMap/);
  assert.match(service, /mutation UpdateProjectMindMap/);
  assert.match(service, /mutation RetryMindMapTask/);
});

test('mind map node long-press menu edits in a dialog and confirms cascading deletion', () => {
  const page = readSource('../src/pages/ProjectMindMapPage.vue');

  assert.match(page, /v-touch-hold\.mouse="\(\) => openNodeMenu\(id\)"/);
  assert.match(page, /<q-menu[\s\S]*no-parent-event[\s\S]*:model-value="menuNodeId === id"/);
  assert.match(page, /<q-item-section>编辑<\/q-item-section>/);
  assert.match(page, /<q-item-section>删除<\/q-item-section>/);
  assert.match(page, /<q-dialog v-model="editDialog">/);
  assert.match(page, /<q-dialog v-model="deleteDialog">/);
  assert.match(page, /确认删除/);
  assert.match(page, /\.map\(\(edge\) => \(\{ kind: 'delete_edge', id: edge\.id \}\)\)/);
  assert.match(page, /operations\.push\(\{ kind: 'delete_node', id: nodeId \}\)/);
});

test('radial layout centers the root, groups connected depths into rings, and curves edges by facing handles', () => {
  const nodes = ['project-root', 'a', 'b', 'c'].map((id) => ({ id }));
  const edges = [
    { id: 'root-a', sourceId: 'project-root', targetId: 'a' },
    { id: 'a-b', sourceId: 'a', targetId: 'b' },
  ];
  const layout = buildRadialLayout(nodes, edges);

  assert.deepEqual(layout['project-root'], { x: -86, y: -24 });
  const radiusA = Math.hypot(layout.a.x + 86, layout.a.y + 24);
  const radiusB = Math.hypot(layout.b.x + 86, layout.b.y + 24);
  const radiusC = Math.hypot(layout.c.x + 86, layout.c.y + 24);
  assert.equal(Math.round(radiusA), 240);
  assert.equal(Math.round(radiusB), 480);
  assert.equal(Math.round(radiusC), 720);
  assert.deepEqual(radialEdgeHandles(edges[0], layout), {
    sourceHandle: 'source-top',
    targetHandle: 'target-bottom',
  });

  const disconnectedLayout = buildRadialLayout([{ id: 'project-root' }, { id: 'new-node' }], []);
  assert.equal(
    Math.round(
      Math.hypot(disconnectedLayout['new-node'].x + 86, disconnectedLayout['new-node'].y + 24),
    ),
    240,
  );
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
