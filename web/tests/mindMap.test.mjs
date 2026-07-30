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
  assert.match(page, /:pan-on-drag="true"/);
  assert.match(page, /:zoom-on-pinch="true"/);
  assert.match(page, /<div class="mind-map-canvas">/);
  assert.doesNotMatch(page, /<PageToolbar[^>]*title-icon=/);
  assert.match(page, /flex: 1 1 auto/);
  assert.match(page, /\.mind-map-canvas\s*\{[^}]*touch-action:\s*none/s);
  assert.match(
    page,
    /\.mind-map-canvas :deep\(\.vue-flow__background\)\s*\{[^}]*pointer-events:\s*none/s,
  );
  assert.match(
    page,
    /\.mind-map-canvas :deep\(\.vue-flow__viewport\),[\s\S]*\.mind-map-canvas :deep\(\.vue-flow__pane\)\s*\{[^}]*touch-action:\s*none/s,
  );
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

test('mind map search matches agent-visible node fields and highlights only matching nodes', () => {
  const page = readSource('../src/pages/ProjectMindMapPage.vue');
  const service = readSource('../src/services/mindMaps.ts');
  const schema = readSource('../../internal/interfaces/graphql/graph/schema.graphqls');

  assert.match(page, /v-model="searchQuery"[\s\S]*placeholder="模拟 Agent 搜索节点"/);
  assert.match(page, /aria-label="搜索思维图节点"/);
  assert.match(page, /\{\{ searchMatchNodeIds\.size \}\} 个/);
  assert.match(page, /searchProjectMindMap\(requestedProjectId, query\)/);
  assert.match(page, /requestRevision !== searchRequestRevision/);
  assert.match(page, /result\.matches\.map\(searchDisplayNodeId\)/);
  assert.match(page, /hasSearch\.value\s*\? searchMatchNodeIds\.value/);
  assert.match(page, /'mind-map-node--search-match'/);
  assert.match(page, /\.mind-map-node--search-match \.mind-map-node-content/);
  assert.match(page, /width: min\(360px, calc\(100% - 24px\)\)/);
  assert.match(service, /query SearchProjectMindMap/);
  assert.match(service, /matches \{ nodeId sessionId \}/);
  assert.match(schema, /searchProjectMindMap\(input: SearchMindMapInput!\): MindMapSearchResult!/);
  assert.doesNotMatch(page, /searchMindMapNodes/);
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
  assert.match(page, /applyOperations\(\[\{ kind: 'delete_node', id: nodeId \}\], sessionId\)/);
  assert.doesNotMatch(page, /\.map\(\(edge\) => \(\{ kind: 'delete_edge'/);
});

test('mind map node information opens on desktop hover and mobile click with a close button', () => {
  const page = readSource('../src/pages/ProjectMindMapPage.vue');

  assert.match(page, /@mouseenter="showNodeInfo\(id\)"/);
  assert.match(page, /if \(\$q\.platform\.is\.mobile\) showNodeInfo\(node\.id\)/);
  assert.match(page, /class="mind-map-node-info"/);
  assert.match(page, /\{\{ data\.label \}\}/);
  assert.match(page, /\{\{ data\.content \|\| '暂无节点内容' \}\}/);
  assert.match(page, /v-if="data\.files\.length"/);
  assert.match(page, /item\.method.*L\{\{ item\.startLine \}\}–\{\{ item\.endLine \}\}/s);
  assert.match(page, /\.mind-map-node-info\s*\{[^}]*overflow-x:\s*hidden/s);
  assert.match(
    page,
    /\.mind-map-node-info__files :deep\(\.q-item__section\)\s*\{[^}]*min-width:\s*0/s,
  );
  assert.match(
    page,
    /\.mind-map-node-info__files :deep\(\.q-item__label\)\s*\{[^}]*overflow-wrap:\s*anywhere/s,
  );
  assert.match(page, /aria-label="关闭节点信息"/);
  assert.match(page, /@click="closeNodeInfo"/);
});

test('mind map combines compact card deltas and previews selected card modifications', () => {
  const page = readSource('../src/pages/ProjectMindMapPage.vue');
  const service = readSource('../src/services/mindMaps.ts');
  const schema = readSource('../../internal/interfaces/graphql/graph/schema.graphqls');

  assert.match(service, /changeType: 'unchanged' \| 'added' \| 'modified' \| 'deleted'/);
  assert.match(
    service,
    /nodes \{ id title content files \{ file method startLine endLine \} changeType \}/,
  );
  assert.match(
    schema,
    /type MindMapNode \{[\s\S]*files: \[MindMapNodeFile!\]![\s\S]*changeType: String!/,
  );
  assert.match(schema, /input MindMapNodeFileInput \{[\s\S]*startLine: Int![\s\S]*endLine: Int!/);
  assert.match(page, /:disable="!nodeTitle\.trim\(\) \|\| invalidNodeFiles"/);
  assert.match(service, /nodes: MindMapNode\[\]/);
  assert.match(service, /modifiedNodeIds: string\[\]/);
  assert.match(service, /deletedNodeIds: string\[\]/);
  assert.match(
    schema,
    /type MindMapCard \{[\s\S]*nodes: \[MindMapNode!\]![\s\S]*edges: \[MindMapEdge!\]![\s\S]*modifiedNodeIds: \[ID!\]![\s\S]*deletedNodeIds: \[ID!\]!/,
  );
  assert.doesNotMatch(page, /<q-select/);
  assert.doesNotMatch(page, /scopeSessionId/);
  assert.match(page, /v-for="card in cards"/);
  assert.match(
    page,
    /combineMindMaps\(mainGraph\.value, cards\.value, activeCardSessionId\.value\)/,
  );
  assert.match(page, /node\.changeType === 'added'/);
  assert.match(page, /node\.changeType === 'modified'/);
  assert.match(page, /activeNode \? activeCardSessionId : ''/);
  assert.match(page, /\.\.\.\(activeNode \?\? node\)/);
  assert.match(page, /cardDisplayId\(card\.sessionId, node\.id\)/);
  assert.match(page, /v-if="cards\.length" class="mind-map-change-legend"/);
  assert.match(page, /data\.cardLabel/);
  assert.match(page, /mind-map-node--added/);
  assert.match(page, /mind-map-node--modified/);
  assert.match(page, /mind-map-node--deleted/);
  assert.match(page, /connectable: node\.changeType !== 'deleted'/);
  assert.match(page, /data\.changeType === 'deleted'/);
  assert.match(page, /var\(--q-positive\)/);
  assert.match(page, /var\(--q-warning\)/);
  assert.match(page, /var\(--q-negative\)/);
});

test('mind map cards toggle related elements with brightness without replacing operation colors', () => {
  const page = readSource('../src/pages/ProjectMindMapPage.vue');

  assert.match(page, /:aria-pressed="activeCardSessionId === card\.sessionId"/);
  assert.match(page, /@click="toggleCardHighlight\(card\.sessionId\)"/);
  assert.match(page, /@keyup\.enter\.self="toggleCardHighlight\(card\.sessionId\)"/);
  assert.match(page, /@keyup\.space\.self\.prevent="toggleCardHighlight\(card\.sessionId\)"/);
  assert.match(page, /const activeCardElementIds = computed/);
  assert.match(page, /card\.modifiedNodeIds, \.\.\.card\.deletedNodeIds/);
  assert.match(page, /node\.changeType === 'added' \? cardDisplayId\(card\.sessionId, node\.id\) : node\.id/);
  assert.match(page, /cardDisplayId\(card\.sessionId, edge\.id\)/);
  assert.match(page, /'mind-map-element--highlighted'/);
  assert.match(page, /\.mind-map-element--highlighted[\s\S]*filter: brightness\(1\.14\)/);
  assert.match(page, /\.mind-map-element--muted[\s\S]*filter: brightness\(0\.58\)/);
  assert.match(page, /\.mind-map-node--added[\s\S]*var\(--q-positive\)/);
  assert.match(page, /\.mind-map-node--modified[\s\S]*var\(--q-warning\)/);
  assert.match(page, /\.mind-map-node--deleted[\s\S]*var\(--q-negative\)/);
  assert.doesNotMatch(page, /mind-map-element--highlighted[^{]*\{[^}]*color:/s);
  assert.doesNotMatch(page, /mind-map-element--highlighted[^{]*\{[^}]*stroke:/s);
});

test('session detail links to its mind map card and the page applies the route highlight once', () => {
  const detail = readSource('../src/components/SessionDetailView.vue');
  const detailPage = readSource('../src/pages/SessionDetailPage.vue');
  const page = readSource('../src/pages/ProjectMindMapPage.vue');

  assert.match(detailPage, /:mind-map-available="mindMapAvailable"/);
  assert.match(detailPage, /mindMapRealtime\.value = mindMapAvailable\.value/);
  assert.match(detail, /name: 'project-mind-map'/);
  assert.match(detail, /params: \{ projectId: session\.value\?\.projectId \?\? '' \}/);
  assert.match(detail, /query: \{ card: sessionId \}/);
  assert.match(detail, /v-if="mindMapAvailable"[\s\S]*label="思维图"[\s\S]*:to="mindMapRoute"/);
  assert.match(page, /let routeCardHighlightApplied = false/);
  assert.match(page, /typeof route\.query\.card === 'string' \? route\.query\.card : ''/);
  assert.match(page, /activeCardSessionId\.value = requestedCardSessionId/);
  assert.match(page, /searchMatchNodeIds\.value = new Set\(result\.matches\.map\(searchDisplayNodeId\)\)/);
  assert.match(page, /node\?\.changeType === 'modified'[\s\S]*\? match\.nodeId/);
});

test('horizontal sessions expose a live mind map link only after the card has effective changes', () => {
  const index = readSource('../src/pages/IndexPage.vue');
  const horizontal = readSource('../src/components/OverviewHorizontalConversation.vue');
  const service = readSource('../src/services/mindMaps.ts');
  const schema = readSource('../../internal/interfaces/graphql/graph/schema.graphqls');

  assert.match(schema, /type MindMapCard \{[\s\S]*hasChanges: Boolean!/);
  assert.match(service, /projectMindMapCards\(projectId: \$projectId\) \{ sessionId hasChanges \}/);
  assert.match(service, /\.filter\(\(card\) => card\.hasChanges\)/);
  assert.match(index, /:mind-map-updated="mindMapUpdated\(card\)"/);
  assert.match(index, /subscribeMindMapUpdates\(projectId, '', \{[\s\S]*refreshProjectMindMapAvailability/);
  assert.match(horizontal, /v-if="mindMapUpdated"[\s\S]*aria-label="打开思维图"/);
  assert.match(horizontal, /params: \{ projectId: card\.projectId \}/);
  assert.match(horizontal, /query: \{ card: card\.id \}/);
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

test('radial layout keeps dense adjacent rings from overlapping', () => {
  const innerNodes = Array.from({ length: 12 }, (_, index) => ({ id: `inner-${index}` }));
  const outerNodes = Array.from({ length: 12 }, (_, index) => ({ id: `outer-${index}` }));
  const nodes = [{ id: 'project-root' }, ...innerNodes, ...outerNodes];
  const edges = innerNodes.flatMap((node, index) => [
    { id: `root-${index}`, sourceId: 'project-root', targetId: node.id },
    { id: `branch-${index}`, sourceId: node.id, targetId: outerNodes[index].id },
  ]);

  const layout = buildRadialLayout(nodes, edges);
  for (let leftIndex = 0; leftIndex < nodes.length; leftIndex += 1) {
    for (let rightIndex = leftIndex + 1; rightIndex < nodes.length; rightIndex += 1) {
      const left = nodes[leftIndex];
      const right = nodes[rightIndex];
      const overlaps =
        Math.abs(layout[left.id].x - layout[right.id].x) < 172 &&
        Math.abs(layout[left.id].y - layout[right.id].y) < 48;
      assert.equal(overlaps, false, `${left.id} overlaps ${right.id}`);
    }
  }
});

test('realtime cards have merge-close while async cards use ordinary close', () => {
  const index = readSource('../src/pages/IndexPage.vue');
  const menu = readSource('../src/components/SessionCardContextMenu.vue');
  const detail = readSource('../src/components/SessionDetailView.vue');
  const sessions = readSource('../src/services/sessions.ts');

  assert.match(index, /generalSettings\.value\.mindMapMode === 'realtime'/);
  assert.match(menu, /v-if="mindMapRealtime"[\s\S]*合并思维图并关闭/);
  assert.match(
    detail,
    /v-if="mindMapRealtime"[\s\S]*class="session-detail-close-button app-command-btn app-on-primary"[\s\S]*unelevated[\s\S]*color="primary"[\s\S]*label="合并思维图并关闭"[\s\S]*:loading="closing"[\s\S]*:disable="!canClose \|\| isClosed \|\| loading \|\| closing"/,
  );
  assert.match(sessions, /reason: 'user_closed' \| 'merged_closed'/);
});

test('GraphQL exposes project main and card graphs plus async task state', () => {
  const schema = readSource('../../internal/interfaces/graphql/graph/schema.graphqls');

  assert.match(schema, /projectMindMap\(input: MindMapPageInput!\): MindMapGraphPage!/);
  assert.match(schema, /projectMindMapCards\(projectId: ID!\): \[MindMapCard!/);
  assert.match(schema, /updateProjectMindMap\(input: UpdateMindMapInput!\): MindMapUpdateEvent!/);
  assert.match(schema, /retryMindMapTask\(id: ID!\): MindMapCard!/);
  assert.match(schema, /mindMapUpdates\(projectId: ID!, sessionId: ID\): MindMapUpdateEvent!/);
  assert.match(schema, /taskStatus: String!/);
  assert.match(schema, /taskError: String!/);
});

test('mind map refresh rejects stale project responses and follows all graph and task changes', () => {
  const page = readSource('../src/pages/ProjectMindMapPage.vue');
  const service = readSource('../src/services/mindMaps.ts');

  assert.match(page, /requestRevision !== graphRequestRevision/);
  assert.match(page, /subscribeMindMapUpdates\(requestedProjectId, '',/);
  assert.doesNotMatch(page, /requestedSessionId/);
  assert.match(page, /card\.taskStatus === 'queued' \|\| card\.taskStatus === 'running'/);
  assert.match(page, /Promise\.all\(\[loadCards\(\), loadGraph\(\)\]\)/);
  assert.match(service, /subscription MindMapUpdates/);
  assert.match(service, /pageSize: 200/);
  assert.match(service, /nextNodeCursor/);
  assert.match(service, /nextEdgeCursor/);
  assert.match(service, /while \(includeNodes \|\| includeEdges\)/);
  assert.match(page, /if \(refreshPromise\) return refreshPromise/);
  assert.match(page, /while \(refreshPending\)/);
  assert.match(page, /update\.updatedAt !== loadedRevision/);
  assert.doesNotMatch(service, /nodes \{ id title content x y \}/);
});
