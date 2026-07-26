<template>
  <q-page class="page-shell mind-map-page">
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
      <q-btn flat round dense icon="refresh" aria-label="刷新思维图" :loading="loading" @click="loadGraph">
        <q-tooltip>刷新</q-tooltip>
      </q-btn>
      <q-btn color="primary" icon="add" label="新增节点" no-caps :disable="loading" @click="addNode" />
    </PageToolbar>

    <q-banner v-if="activeCard?.taskStatus" dense rounded class="mind-map-task-banner">
      <template #avatar><q-icon name="manage_history" color="primary" /></template>
      异步整理：{{ taskStatusLabel(activeCard.taskStatus) }}
      <span v-if="activeCard.taskError"> · {{ activeCard.taskError }}</span>
      <template v-if="activeCard.taskStatus === 'failed' && activeCard.taskId" #action>
        <q-btn flat dense icon="refresh" label="重试" no-caps @click="retryTask(activeCard.taskId)" />
      </template>
    </q-banner>

    <div class="mind-map-layout">
      <q-card flat bordered class="mind-map-canvas">
        <VueFlow
          id="project-mind-map-flow"
          v-model:nodes="flowNodes"
          v-model:edges="flowEdges"
          :min-zoom="0.2"
          :max-zoom="2"
          :nodes-connectable="true"
          :elements-selectable="true"
          fit-view-on-init
          @node-click="selectNode"
          @edge-click="selectEdge"
          @node-drag-stop="saveNodePosition"
          @connect="createEdge"
        >
          <Background pattern-color="var(--ac-border)" :gap="24" />
          <Controls position="top-right" />
        </VueFlow>
      </q-card>

      <q-card flat bordered class="mind-map-editor">
        <q-card-section class="row items-center">
          <div class="text-subtitle1 text-weight-bold">编辑</div>
          <q-space />
          <q-chip v-if="scopeSessionId" dense outline color="primary">卡片隔离视图</q-chip>
          <q-chip v-else dense outline color="positive">项目主图</q-chip>
        </q-card-section>
        <q-separator />
        <q-card-section v-if="selectedNode" class="q-gutter-md">
          <q-input v-model="nodeTitle" dense outlined label="节点标题" :disable="rootSelected" />
          <q-input v-model="nodeContent" outlined autogrow type="textarea" label="节点内容" :disable="rootSelected" />
          <div class="row justify-end q-gutter-sm">
            <q-btn
              v-if="!rootSelected"
              flat
              color="negative"
              icon="delete"
              label="删除"
              no-caps
              @click="deleteNode"
            />
            <q-btn
              color="primary"
              icon="save"
              label="保存"
              no-caps
              :disable="rootSelected || !nodeTitle.trim()"
              @click="saveNode"
            />
          </div>
          <q-banner v-if="rootSelected" dense rounded>项目名根节点固定在中心，只能作为关系端点。</q-banner>
        </q-card-section>
        <q-card-section v-else-if="selectedEdge" class="q-gutter-md">
          <q-input v-model="edgeLabel" dense outlined label="关系说明" />
          <div class="row justify-end q-gutter-sm">
            <q-btn flat color="negative" icon="delete" label="删除" no-caps @click="deleteEdge" />
            <q-btn color="primary" icon="save" label="保存" no-caps @click="saveEdge" />
          </div>
        </q-card-section>
        <q-card-section v-else>
          <q-banner rounded class="empty-lane-banner">选择节点或关系进行编辑，也可拖动连接点创建关系。</q-banner>
        </q-card-section>
      </q-card>
    </div>

    <q-inner-loading :showing="loading"><q-spinner color="primary" size="32px" /></q-inner-loading>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import { Background } from '@vue-flow/background';
import { Controls } from '@vue-flow/controls';
import {
  useVueFlow,
  VueFlow,
  type Connection,
  type EdgeMouseEvent,
  type NodeDragEvent,
  type NodeMouseEvent,
} from '@vue-flow/core';
import '@vue-flow/core/dist/style.css';
import '@vue-flow/core/dist/theme-default.css';
import '@vue-flow/controls/dist/style.css';

import PageToolbar from '@/components/PageToolbar.vue';
import { useProjects } from '@/composables/useProjects';
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
const graph = ref<MindMapGraph>({ projectId: '', nodes: [], edges: [], updatedAt: '' });
const cards = ref<MindMapCard[]>([]);
const scopeSessionId = ref('');
const selectedNodeId = ref('');
const selectedEdgeId = ref('');
const nodeTitle = ref('');
const nodeContent = ref('');
const edgeLabel = ref('');

const scopeOptions = computed(() => [
  { label: '项目主图', value: '' },
  ...cards.value.map((card) => ({
    label: `${card.requirement || card.sessionId}${card.taskStatus ? ` · ${taskStatusLabel(card.taskStatus)}` : ''}`,
    value: card.sessionId,
  })),
]);
const activeCard = computed(() => cards.value.find((card) => card.sessionId === scopeSessionId.value));
const selectedNode = computed(() => graph.value.nodes.find((node) => node.id === selectedNodeId.value));
const selectedEdge = computed(() => graph.value.edges.find((edge) => edge.id === selectedEdgeId.value));
const rootSelected = computed(() => selectedNodeId.value === rootNodeId);
const flowNodes = computed({
  get: () =>
    graph.value.nodes.map((node) => ({
      id: node.id,
      label: node.title,
      data: { label: node.title },
      position: { x: node.x, y: node.y },
      draggable: node.id !== rootNodeId,
    })),
  set: (nodes) => {
    for (const flowNode of nodes) {
      const node = graph.value.nodes.find((item) => item.id === flowNode.id);
      if (node && node.id !== rootNodeId) Object.assign(node, flowNode.position);
    }
  },
});
const flowEdges = computed({
  get: () =>
    graph.value.edges.map((edge) => ({
      id: edge.id,
      source: edge.sourceId,
      target: edge.targetId,
      label: edge.label,
      type: 'smoothstep',
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
    requestAnimationFrame(() => void fitView({ padding: 0.2 }));
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
  await loadCards();
}

async function addNode() {
  const id = crypto.randomUUID();
  await applyOperations([
    { kind: 'upsert_node', id, title: '新节点', content: '', x: 220, y: graph.value.nodes.length * 72 },
  ]);
  selectedNodeId.value = id;
  syncNodeDraft();
}

function selectNode({ node }: NodeMouseEvent) {
  selectedNodeId.value = node.id;
  selectedEdgeId.value = '';
  syncNodeDraft();
}

function selectEdge({ edge }: EdgeMouseEvent) {
  selectedEdgeId.value = edge.id;
  selectedNodeId.value = '';
  edgeLabel.value = graph.value.edges.find((item) => item.id === edge.id)?.label ?? '';
}

function syncNodeDraft() {
  nodeTitle.value = selectedNode.value?.title ?? '';
  nodeContent.value = selectedNode.value?.content ?? '';
}

async function saveNode() {
  if (!selectedNode.value || rootSelected.value || !nodeTitle.value.trim()) return;
  await applyOperations([
    { kind: 'upsert_node', id: selectedNode.value.id, title: nodeTitle.value, content: nodeContent.value },
  ]);
}

async function deleteNode() {
  if (!selectedNode.value || rootSelected.value) return;
  const nodeId = selectedNode.value.id;
  const operations: MindMapOperation[] = graph.value.edges
    .filter((edge) => edge.sourceId === nodeId || edge.targetId === nodeId)
    .map((edge) => ({ kind: 'delete_edge', id: edge.id }));
  operations.push({ kind: 'delete_node', id: nodeId });
  await applyOperations(operations);
  clearSelection();
}

async function saveNodePosition({ node }: NodeDragEvent) {
  if (node.id === rootNodeId) return;
  await applyOperations([
    { kind: 'upsert_node', id: node.id, x: node.position.x, y: node.position.y },
  ]);
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

async function saveEdge() {
  if (!selectedEdge.value) return;
  await applyOperations([{ kind: 'upsert_edge', id: selectedEdge.value.id, label: edgeLabel.value }]);
}

async function deleteEdge() {
  if (!selectedEdge.value) return;
  await applyOperations([{ kind: 'delete_edge', id: selectedEdge.value.id }]);
  clearSelection();
}

async function retryTask(taskId: string) {
  await retryMindMapTask(taskId);
  await loadCards();
}

function clearSelection() {
  selectedNodeId.value = '';
  selectedEdgeId.value = '';
}

function taskStatusLabel(status: string) {
  return { queued: '排队中', running: '整理中', failed: '失败', completed: '已完成' }[status] ?? status;
}
</script>

<style scoped>
.mind-map-page {
  display: flex;
  min-height: 0;
  flex-direction: column;
}

.mind-map-scope {
  width: min(340px, 45vw);
}

.mind-map-task-banner {
  margin: 8px 16px 0;
}

.mind-map-layout {
  display: grid;
  min-height: 0;
  flex: 1 1 auto;
  grid-template-columns: minmax(0, 1fr) 320px;
  gap: 12px;
  padding: 12px 16px 16px;
}

.mind-map-canvas {
  min-height: 560px;
  overflow: hidden;
}

.mind-map-canvas :deep(.vue-flow) {
  height: 100%;
}

.mind-map-editor {
  min-width: 0;
}

@media (max-width: 699px) {
  .mind-map-layout {
    grid-template-columns: 1fr;
  }

  .mind-map-canvas {
    min-height: 55dvh;
  }

  .mind-map-scope {
    width: 180px;
  }
}
</style>
