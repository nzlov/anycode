<template>
  <q-page class="mind-map-page">
    <PageToolbar :title="project ? `${project.name} · 思维图` : '项目思维图'" title-icon="hub">
      <q-select
        v-model="scopeSessionId"
        dense
        outlined
        emit-value
        map-options
        options-dense
        :options="scopeOptions"
        aria-label="思维图范围"
        class="mind-map-scope"
      />
      <q-btn
        flat
        round
        dense
        icon="refresh"
        aria-label="刷新思维图"
        :loading="loading"
        @click="loadGraph"
      >
        <q-tooltip>刷新</q-tooltip>
      </q-btn>
      <q-btn
        color="primary"
        icon="add"
        label="新增节点"
        no-caps
        :disable="loading"
        @click="openNewNodeDialog"
      />
    </PageToolbar>

    <q-banner v-if="activeCard?.taskStatus" dense rounded class="mind-map-task-banner">
      <template #avatar><q-icon name="manage_history" color="primary" /></template>
      异步整理：{{ taskStatusLabel(activeCard.taskStatus) }}
      <span v-if="activeCard.taskError"> · {{ activeCard.taskError }}</span>
      <template v-if="activeCard.taskStatus === 'failed' && activeCard.taskId" #action>
        <q-btn
          flat
          dense
          icon="refresh"
          label="重试"
          no-caps
          @click="retryTask(activeCard.taskId)"
        />
      </template>
    </q-banner>

    <div class="mind-map-canvas">
      <VueFlow
        id="project-mind-map-flow"
        v-model:nodes="flowNodes"
        v-model:edges="flowEdges"
        :min-zoom="0.2"
        :max-zoom="2"
        :nodes-connectable="true"
        :elements-selectable="true"
        @node-click="selectNode"
        @nodes-initialized="fitGraph"
        @pane-click="clearSelection"
        @connect="createEdge"
      >
        <template #node-radial="{ id, data }">
          <div
            v-touch-hold.mouse="() => openNodeMenu(id)"
            class="mind-map-node-content"
            @contextmenu.prevent="openNodeMenu(id)"
          >
            <Handle
              v-for="side in handleSides"
              :id="`target-${side}`"
              :key="`target-${side}`"
              type="target"
              :position="handlePosition(side)"
            />
            <Handle
              v-for="side in handleSides"
              :id="`source-${side}`"
              :key="`source-${side}`"
              type="source"
              :position="handlePosition(side)"
            />
            <span>{{ data.label }}</span>
            <q-menu
              no-parent-event
              :model-value="menuNodeId === id"
              @update:model-value="syncNodeMenu(id, $event)"
              @click.stop
            >
              <q-list dense class="app-touch-list mind-map-node-menu">
                <q-item-label header>节点操作</q-item-label>
                <q-item
                  v-close-popup
                  clickable
                  :disable="id === rootNodeId"
                  @click="openNodeEditor(id)"
                >
                  <q-item-section avatar><q-icon name="edit" /></q-item-section>
                  <q-item-section>编辑</q-item-section>
                </q-item>
                <q-item
                  v-close-popup
                  clickable
                  :disable="id === rootNodeId"
                  class="text-negative"
                  @click="openDeleteDialog(id)"
                >
                  <q-item-section avatar><q-icon name="delete" /></q-item-section>
                  <q-item-section>删除</q-item-section>
                </q-item>
              </q-list>
            </q-menu>
          </div>
        </template>
        <Background pattern-color="var(--ac-border)" :gap="24" />
        <Controls position="top-right" />
      </VueFlow>
    </div>

    <q-dialog v-model="editDialog">
      <q-card class="mind-map-dialog">
        <q-form @submit.prevent="saveNode">
          <q-card-section>
            <div class="text-subtitle1 text-weight-bold">
              {{ editingNodeId ? '编辑节点' : '新增节点' }}
            </div>
          </q-card-section>
          <q-card-section class="q-gutter-md">
            <q-input v-model="nodeTitle" dense outlined autofocus label="节点标题" />
            <q-input v-model="nodeContent" outlined autogrow type="textarea" label="节点内容" />
          </q-card-section>
          <q-card-actions align="right">
            <q-btn v-close-popup flat label="取消" no-caps />
            <q-btn
              color="primary"
              type="submit"
              label="保存"
              no-caps
              :loading="saving"
              :disable="!nodeTitle.trim()"
            />
          </q-card-actions>
        </q-form>
      </q-card>
    </q-dialog>

    <q-dialog v-model="deleteDialog">
      <q-card class="confirm-dialog mind-map-dialog">
        <q-card-section>
          <div class="text-subtitle1 text-weight-bold">删除节点</div>
        </q-card-section>
        <q-card-section>
          确认删除“{{ deletingNode?.title }}”吗？与该节点直接关联的
          {{ deletingEdgeCount }} 条连线也会同步删除。
        </q-card-section>
        <q-card-actions align="right">
          <q-btn v-close-popup flat label="取消" no-caps />
          <q-btn color="negative" label="删除" no-caps :loading="saving" @click="deleteNode" />
        </q-card-actions>
      </q-card>
    </q-dialog>

    <q-inner-loading :showing="loading"><q-spinner color="primary" size="32px" /></q-inner-loading>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import { Background } from '@vue-flow/background';
import { Controls } from '@vue-flow/controls';
import {
  Handle,
  Position,
  useVueFlow,
  VueFlow,
  type Connection,
  type NodeMouseEvent,
} from '@vue-flow/core';
import '@vue-flow/core/dist/style.css';
import '@vue-flow/core/dist/theme-default.css';
import '@vue-flow/controls/dist/style.css';

import PageToolbar from '@/components/PageToolbar.vue';
import { useProjects } from '@/composables/useProjects';
import { buildRadialLayout, radialEdgeHandles } from '@/services/mindMapFlowModel';
import {
  getProjectMindMap,
  listProjectMindMapCards,
  retryMindMapTask,
  updateProjectMindMap,
  type MindMapCard,
  type MindMapGraph,
  type MindMapOperation,
} from '@/services/mindMaps';

const rootNodeId = 'project-root';
const route = useRoute();
const { projects, loadProjects } = useProjects();
const { fitView } = useVueFlow('project-mind-map-flow');
const projectId = computed(() => String(route.params.projectId ?? ''));
const project = computed(() => projects.value.find((item) => item.id === projectId.value));
const loading = ref(false);
const saving = ref(false);
const graph = ref<MindMapGraph>({ projectId: '', nodes: [], edges: [], updatedAt: '' });
const cards = ref<MindMapCard[]>([]);
const scopeSessionId = ref('');
const selectedNodeId = ref('');
const menuNodeId = ref('');
const editingNodeId = ref('');
const deletingNodeId = ref('');
const editDialog = ref(false);
const deleteDialog = ref(false);
const nodeTitle = ref('');
const nodeContent = ref('');
const handleSides = ['top', 'right', 'bottom', 'left'] as const;

const scopeOptions = computed(() => [
  { label: '项目主图', value: '' },
  ...cards.value.map((card) => ({
    label: `${card.requirement || card.sessionId}${card.taskStatus ? ` · ${taskStatusLabel(card.taskStatus)}` : ''}`,
    value: card.sessionId,
  })),
]);
const activeCard = computed(() =>
  cards.value.find((card) => card.sessionId === scopeSessionId.value),
);
const radialLayout = computed(() =>
  buildRadialLayout(graph.value.nodes, graph.value.edges, rootNodeId),
);
const directlyRelatedNodeIds = computed(() => {
  const related = new Set<string>();
  if (!selectedNodeId.value) return related;
  related.add(selectedNodeId.value);
  for (const edge of graph.value.edges) {
    if (edge.sourceId === selectedNodeId.value) related.add(edge.targetId);
    if (edge.targetId === selectedNodeId.value) related.add(edge.sourceId);
  }
  return related;
});
const directlyRelatedEdgeIds = computed(() => {
  if (!selectedNodeId.value) return new Set<string>();
  return new Set(
    graph.value.edges
      .filter(
        (edge) => edge.sourceId === selectedNodeId.value || edge.targetId === selectedNodeId.value,
      )
      .map((edge) => edge.id),
  );
});
const deletingNode = computed(() =>
  graph.value.nodes.find((node) => node.id === deletingNodeId.value),
);
const deletingEdgeCount = computed(
  () =>
    graph.value.edges.filter(
      (edge) => edge.sourceId === deletingNodeId.value || edge.targetId === deletingNodeId.value,
    ).length,
);
const flowNodes = computed({
  get: () =>
    graph.value.nodes.map((node) => ({
      id: node.id,
      type: 'radial',
      label: node.title,
      data: { label: node.title },
      position: radialLayout.value[node.id] ?? { x: 0, y: 0 },
      draggable: false,
      class: {
        'mind-map-node--root': node.id === rootNodeId,
        'mind-map-node--active': node.id === selectedNodeId.value,
        'mind-map-node--related':
          Boolean(selectedNodeId.value) &&
          node.id !== selectedNodeId.value &&
          directlyRelatedNodeIds.value.has(node.id),
        'mind-map-element--muted':
          Boolean(selectedNodeId.value) && !directlyRelatedNodeIds.value.has(node.id),
      },
    })),
  set: () => undefined,
});
const flowEdges = computed({
  get: () =>
    graph.value.edges.map((edge) => ({
      id: edge.id,
      source: edge.sourceId,
      target: edge.targetId,
      label: edge.label,
      type: 'default',
      ...radialEdgeHandles(edge, radialLayout.value),
      class: {
        'mind-map-edge--active': directlyRelatedEdgeIds.value.has(edge.id),
        'mind-map-element--muted':
          Boolean(selectedNodeId.value) && !directlyRelatedEdgeIds.value.has(edge.id),
      },
    })),
  set: () => undefined,
});

onMounted(async () => {
  await loadProjects();
  await Promise.all([loadCards(), loadGraph()]);
});

watch(scopeSessionId, () => void loadGraph());

async function loadCards() {
  cards.value = await listProjectMindMapCards(projectId.value);
}

async function loadGraph() {
  if (!projectId.value || loading.value) return;
  loading.value = true;
  try {
    graph.value = await getProjectMindMap(projectId.value, scopeSessionId.value);
    clearSelection();
    fitGraph();
  } finally {
    loading.value = false;
  }
}

async function applyOperations(operations: MindMapOperation[]) {
  graph.value = await updateProjectMindMap({
    projectId: projectId.value,
    ...(scopeSessionId.value ? { sessionId: scopeSessionId.value } : {}),
    operations,
  });
  fitGraph();
  await loadCards();
}

function fitGraph() {
  requestAnimationFrame(() => void fitView({ padding: 0.2 }));
}

function selectNode({ node }: NodeMouseEvent) {
  selectedNodeId.value = node.id;
}

function openNodeMenu(nodeId: string) {
  selectedNodeId.value = nodeId;
  menuNodeId.value = nodeId;
}

function syncNodeMenu(nodeId: string, open: boolean) {
  if (open) {
    openNodeMenu(nodeId);
  } else if (menuNodeId.value === nodeId) {
    menuNodeId.value = '';
  }
}

function openNewNodeDialog() {
  editingNodeId.value = '';
  nodeTitle.value = '';
  nodeContent.value = '';
  editDialog.value = true;
}

function openNodeEditor(nodeId: string) {
  const node = graph.value.nodes.find((item) => item.id === nodeId);
  if (!node || node.id === rootNodeId) return;
  editingNodeId.value = node.id;
  nodeTitle.value = node.title;
  nodeContent.value = node.content;
  editDialog.value = true;
}

function openDeleteDialog(nodeId: string) {
  if (nodeId === rootNodeId || !graph.value.nodes.some((node) => node.id === nodeId)) return;
  deletingNodeId.value = nodeId;
  deleteDialog.value = true;
}

async function saveNode() {
  const title = nodeTitle.value.trim();
  if (!title || editingNodeId.value === rootNodeId) return;
  saving.value = true;
  try {
    const nodeId = editingNodeId.value || crypto.randomUUID();
    const operation: MindMapOperation = {
      kind: 'upsert_node',
      id: nodeId,
      title,
      content: nodeContent.value,
      ...(editingNodeId.value ? {} : { x: 0, y: 0 }),
    };
    await applyOperations([operation]);
    selectedNodeId.value = nodeId;
    editDialog.value = false;
  } finally {
    saving.value = false;
  }
}

async function deleteNode() {
  if (!deletingNode.value || deletingNode.value.id === rootNodeId) return;
  const nodeId = deletingNode.value.id;
  const operations: MindMapOperation[] = graph.value.edges
    .filter((edge) => edge.sourceId === nodeId || edge.targetId === nodeId)
    .map((edge) => ({ kind: 'delete_edge', id: edge.id }));
  operations.push({ kind: 'delete_node', id: nodeId });
  saving.value = true;
  try {
    await applyOperations(operations);
    deleteDialog.value = false;
    deletingNodeId.value = '';
    clearSelection();
  } finally {
    saving.value = false;
  }
}

async function createEdge(connection: Connection) {
  if (!connection.source || !connection.target || connection.source === connection.target) return;
  await applyOperations([
    {
      kind: 'upsert_edge',
      id: crypto.randomUUID(),
      sourceId: connection.source,
      targetId: connection.target,
      label: '',
    },
  ]);
}

function handlePosition(side: (typeof handleSides)[number]) {
  return { top: Position.Top, right: Position.Right, bottom: Position.Bottom, left: Position.Left }[
    side
  ];
}

async function retryTask(taskId: string) {
  await retryMindMapTask(taskId);
  await loadCards();
}

function clearSelection() {
  selectedNodeId.value = '';
}

function taskStatusLabel(status: string) {
  return (
    { queued: '排队中', running: '整理中', failed: '失败', completed: '已完成' }[status] ?? status
  );
}
</script>

<style scoped>
.mind-map-page {
  position: relative;
  display: flex;
  min-height: 0;
  flex-direction: column;
  overflow: hidden;
}

.mind-map-scope {
  width: min(340px, 45vw);
}

.mind-map-task-banner {
  position: absolute;
  z-index: 5;
  top: 12px;
  left: 16px;
  max-width: calc(100% - 96px);
}

.mind-map-canvas {
  position: relative;
  min-height: 0;
  flex: 1 1 auto;
  overflow: hidden;
  border-radius: 0;
}

.mind-map-canvas :deep(.vue-flow) {
  position: absolute;
  inset: 0;
  width: auto;
  height: auto;
}

.mind-map-node-content {
  display: flex;
  width: 172px;
  min-height: 48px;
  align-items: center;
  justify-content: center;
  padding: 10px 14px;
  border: 1px solid var(--ac-border);
  border-radius: 24px;
  background: var(--ac-surface);
  box-shadow: var(--ac-shadow-card);
  color: var(--ac-text);
  font-size: 13px;
  font-weight: 600;
  text-align: center;
  transition:
    border-color 160ms ease,
    box-shadow 160ms ease,
    opacity 160ms ease;
}

.mind-map-node-content > span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mind-map-canvas :deep(.vue-flow__node) {
  transition: opacity 160ms ease;
}

.mind-map-canvas :deep(.mind-map-node--root .mind-map-node-content) {
  border-color: var(--q-primary);
  background: var(--q-primary);
  color: white;
}

.mind-map-canvas :deep(.mind-map-node--active .mind-map-node-content) {
  border-color: var(--q-primary);
  box-shadow:
    0 0 0 3px color-mix(in srgb, var(--q-primary) 24%, transparent),
    var(--ac-shadow-card);
}

.mind-map-canvas :deep(.mind-map-node--related .mind-map-node-content) {
  border-color: var(--q-primary);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--q-primary) 14%, transparent);
}

.mind-map-canvas :deep(.mind-map-element--muted) {
  opacity: 0.16;
}

.mind-map-canvas :deep(.vue-flow__edge-path) {
  transition:
    stroke 160ms ease,
    stroke-width 160ms ease,
    opacity 160ms ease;
}

.mind-map-canvas :deep(.mind-map-edge--active .vue-flow__edge-path) {
  stroke: var(--q-primary);
  stroke-width: 3;
}

.mind-map-canvas :deep(.vue-flow__handle) {
  width: 7px;
  height: 7px;
  border-color: var(--ac-surface);
  background: var(--q-primary);
}

.mind-map-canvas :deep(.vue-flow__handle.target) {
  opacity: 0;
}

.mind-map-dialog {
  width: min(480px, calc(100vw - 32px));
}

.mind-map-node-menu {
  min-width: 160px;
}

@media (max-width: 699px) {
  .mind-map-scope {
    width: 180px;
  }

  .mind-map-task-banner {
    left: 8px;
    max-width: calc(100% - 64px);
  }
}
</style>
