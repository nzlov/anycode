<template>
  <q-page class="mind-map-page">
    <PageToolbar
      :title="project ? `${project.name} · 思维图` : '项目思维图'"
      compact-title-on-mobile
    >
      <q-input
        v-model="searchQuery"
        class="mind-map-toolbar-search"
        dense
        outlined
        clearable
        debounce="200"
        :loading="searchLoading"
        placeholder="模拟 Agent 搜索节点"
        aria-label="搜索思维图节点"
      >
        <template #prepend><q-icon name="search" /></template>
        <template v-if="hasSearch" #append>
          <span class="mind-map-toolbar-search__count" aria-live="polite">
            {{ searchMatchNodeIds.size }} 个
          </span>
        </template>
      </q-input>
      <q-btn
        flat
        round
        dense
        icon="account_tree"
        :aria-label="`临时切换思维图布局，当前为${activeLayoutLabel}`"
      >
        <q-tooltip>布局：{{ activeLayoutLabel }}</q-tooltip>
        <q-menu>
          <q-list dense class="app-touch-list">
            <q-item-label header>临时布局</q-item-label>
            <q-item
              v-for="option in mindMapLayoutOptions"
              :key="option.value"
              v-close-popup
              clickable
              :active="activeLayoutAlgorithm === option.value"
              @click="selectTemporaryLayout(option.value)"
            >
              <q-item-section>{{ option.label }}</q-item-section>
              <q-item-section v-if="activeLayoutAlgorithm === option.value" side>
                <q-icon name="check" color="primary" />
              </q-item-section>
            </q-item>
          </q-list>
        </q-menu>
      </q-btn>
      <q-btn
        flat
        round
        dense
        icon="refresh"
        aria-label="刷新思维图"
        :loading="loading"
        @click="refreshMindMap"
      >
        <q-tooltip>刷新</q-tooltip>
      </q-btn>
      <q-btn
        color="primary"
        icon="add"
        :label="$q.screen.lt.sm ? undefined : '新增节点'"
        :round="$q.screen.lt.sm"
        aria-label="新增节点"
        no-caps
        :disable="loading"
        @click="openNewNodeDialog"
      />
    </PageToolbar>

    <section v-if="cards.length" class="mind-map-card-strip" aria-label="卡片思维图">
      <q-card
        v-for="card in cards"
        :key="card.sessionId"
        flat
        bordered
        clickable
        tabindex="0"
        role="button"
        class="mind-map-card"
        :class="{ 'mind-map-card--active': activeCardSessionId === card.sessionId }"
        :aria-pressed="activeCardSessionId === card.sessionId"
        @click="toggleCardHighlight(card.sessionId)"
        @keyup.enter.self="toggleCardHighlight(card.sessionId)"
        @keyup.space.self.prevent="toggleCardHighlight(card.sessionId)"
      >
        <q-card-section class="mind-map-card__body">
          <div class="mind-map-card__badges">
            <q-badge
              v-if="card.taskStatus"
              outline
              :color="taskStatusColor(card.taskStatus)"
              :label="taskStatusLabel(card.taskStatus)"
            />
            <span class="text-positive"
              >+{{ card.nodes.filter((node) => node.changeType === 'added').length }}</span
            >
            <span class="text-warning">~{{ card.modifiedNodeIds.length }}</span>
            <span class="text-negative">−{{ card.deletedNodeIds.length }}</span>
          </div>
          <div class="mind-map-card__title">{{ card.requirement || card.sessionId }}</div>
          <div v-if="card.taskError" class="mind-map-card__error">{{ card.taskError }}</div>
          <q-btn
            v-if="card.taskStatus === 'failed' && card.taskId"
            flat
            round
            dense
            icon="refresh"
            aria-label="重试思维图整理"
            class="mind-map-card__retry"
            @click.stop="retryTask(card.taskId)"
          />
        </q-card-section>
      </q-card>
    </section>

    <div v-if="cards.length" class="mind-map-change-legend" aria-label="节点变更状态">
      <span><i class="mind-map-change-legend__dot mind-map-change-legend__dot--added" />新增</span>
      <span
        ><i class="mind-map-change-legend__dot mind-map-change-legend__dot--modified" />修改</span
      >
      <span
        ><i class="mind-map-change-legend__dot mind-map-change-legend__dot--deleted" />删除</span
      >
    </div>

    <div class="mind-map-canvas">
      <VueFlow
        id="project-mind-map-flow"
        v-model:nodes="flowNodes"
        v-model:edges="flowEdges"
        :min-zoom="0.2"
        :max-zoom="2"
        :pan-on-drag="true"
        :zoom-on-pinch="true"
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
            @mouseenter="showNodeInfo(id)"
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
            <span class="mind-map-node-content__title">{{ data.label }}</span>
            <small v-if="data.isTag" class="mind-map-node-content__tag">TAG</small>
            <small v-if="data.cardLabel" class="mind-map-node-content__card">
              {{ data.cardLabel }}
            </small>
            <q-menu
              no-parent-event
              anchor="top right"
              self="top left"
              :offset="[8, 0]"
              :model-value="infoNodeId === id"
              @update:model-value="syncNodeInfo(id, $event)"
            >
              <q-card class="mind-map-node-info" @click.stop>
                <q-card-section class="row items-start no-wrap q-pb-sm">
                  <div class="mind-map-node-info__title">{{ data.label }}</div>
                  <q-space />
                  <q-btn
                    v-close-popup
                    flat
                    round
                    dense
                    icon="close"
                    aria-label="关闭节点信息"
                    @click="closeNodeInfo"
                  />
                </q-card-section>
                <q-card-section class="mind-map-node-info__content q-pt-none">
                  {{ data.content || '暂无节点内容' }}
                </q-card-section>
                <template v-if="data.files.length">
                  <q-separator />
                  <q-list dense class="mind-map-node-info__files">
                    <q-item
                      v-for="item in data.files"
                      :key="`${item.file}:${item.method}:${item.startLine}:${item.endLine}`"
                    >
                      <q-item-section>
                        <q-item-label class="text-weight-medium">{{ item.file }}</q-item-label>
                        <q-item-label caption>
                          {{ item.method }} · L{{ item.startLine }}–{{ item.endLine }}
                        </q-item-label>
                      </q-item-section>
                    </q-item>
                  </q-list>
                </template>
              </q-card>
            </q-menu>
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
                  :disable="id === rootNodeId || data.changeType === 'deleted'"
                  @click="openNodeEditor(id)"
                >
                  <q-item-section avatar><q-icon name="edit" /></q-item-section>
                  <q-item-section>编辑</q-item-section>
                </q-item>
                <q-item
                  v-close-popup
                  clickable
                  :disable="id === rootNodeId || data.changeType === 'deleted'"
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
            <q-input v-model="nodeTitle" dense outlined autofocus label="节点标题 *" />
            <q-input v-model="nodeContent" outlined autogrow type="textarea" label="节点内容" />
            <div class="row items-center">
              <div class="text-subtitle2">文件位置（可选）</div>
              <q-space />
              <q-btn
                flat
                dense
                no-caps
                icon="add"
                label="添加"
                :disable="nodeFiles.length >= 100"
                @click="addNodeFile"
              />
            </div>
            <div v-for="(item, index) in nodeFiles" :key="index" class="mind-map-file-editor">
              <q-input v-model="item.file" dense outlined label="文件 *" />
              <q-input v-model="item.method" dense outlined label="方法 *" />
              <q-input
                v-model.number="item.startLine"
                dense
                outlined
                type="number"
                min="1"
                label="起始行 *"
              />
              <q-input
                v-model.number="item.endLine"
                dense
                outlined
                type="number"
                min="1"
                label="结束行 *"
              />
              <q-btn
                flat
                round
                dense
                color="negative"
                icon="delete"
                aria-label="删除文件位置"
                @click="nodeFiles.splice(index, 1)"
              />
            </div>
          </q-card-section>
          <q-card-actions align="right">
            <q-btn v-close-popup flat label="取消" no-caps />
            <q-btn
              color="primary"
              type="submit"
              label="保存"
              no-caps
              :loading="saving"
              :disable="!nodeTitle.trim() || invalidNodeFiles"
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
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useQuasar } from 'quasar';
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
import {
  buildNestedLayout,
  buildRadialLayout,
  radialEdgeHandles,
} from '@/services/mindMapFlowModel';
import {
  getGeneralSettings,
  type MindMapLayout,
  mindMapLayoutOptions,
} from '@/services/generalSettings';
import {
  getProjectMindMap,
  listProjectMindMapCards,
  retryMindMapTask,
  searchProjectMindMap,
  subscribeMindMapUpdates,
  updateProjectMindMap,
  type MindMapCard,
  type MindMapEdge,
  type MindMapGraph,
  type MindMapNode,
  type MindMapNodeFile,
  type MindMapOperation,
} from '@/services/mindMaps';

type DisplayMindMapNode = MindMapNode & {
  entityId: string;
  sessionId: string;
  cardLabel: string;
};

type DisplayMindMapEdge = MindMapEdge & {
  entityId: string;
  sessionId: string;
};

type DisplayMindMapGraph = Omit<MindMapGraph, 'nodes' | 'edges'> & {
  nodes: DisplayMindMapNode[];
  edges: DisplayMindMapEdge[];
};

const rootNodeId = 'project-root';
const $q = useQuasar();
const route = useRoute();
const { projects, loadProjects } = useProjects();
const { fitView } = useVueFlow('project-mind-map-flow');
const projectId = computed(() => String(route.params.projectId ?? ''));
const project = computed(() => projects.value.find((item) => item.id === projectId.value));
const loading = ref(false);
const saving = ref(false);
const mainGraph = ref<MindMapGraph>({ projectId: '', nodes: [], edges: [], updatedAt: '' });
const cards = ref<MindMapCard[]>([]);
const activeCardSessionId = ref('');
const activeLayoutAlgorithm = ref<MindMapLayout>('radial');
let routeCardHighlightApplied = false;
const searchQuery = ref('');
const searchLoading = ref(false);
const searchMatchNodeIds = ref<Set<string>>(new Set());
const selectedNodeId = ref('');
const infoNodeId = ref('');
const menuNodeId = ref('');
const editingNodeId = ref('');
const deletingNodeId = ref('');
const editDialog = ref(false);
const deleteDialog = ref(false);
const nodeTitle = ref('');
const nodeContent = ref('');
const nodeFiles = ref<MindMapNodeFile[]>([]);
const handleSides = ['top', 'right', 'bottom', 'left'] as const;
let graphRequestRevision = 0;
let cardRequestRevision = 0;
let searchRequestRevision = 0;
let subscriptionRevision = 0;
let mindMapSubscription: { unsubscribe: () => void } | null = null;
let subscriptionReconnectTimer: ReturnType<typeof setTimeout> | null = null;
let taskRefreshTimer: ReturnType<typeof setTimeout> | null = null;
let refreshPromise: Promise<void> | null = null;
let refreshPending = false;
let loadedRevision = '';
let disposed = false;

const graph = computed<DisplayMindMapGraph>(() =>
  combineMindMaps(mainGraph.value, cards.value, activeCardSessionId.value),
);
const activeLayout = computed(() =>
  activeLayoutAlgorithm.value === 'nested'
    ? buildNestedLayout(graph.value.nodes, graph.value.edges, rootNodeId)
    : buildRadialLayout(graph.value.nodes, graph.value.edges, rootNodeId),
);
const activeLayoutLabel = computed(
  () =>
    mindMapLayoutOptions.find((option) => option.value === activeLayoutAlgorithm.value)?.label ??
    '',
);
const hasSearch = computed(() => Boolean(searchQuery.value?.trim()));
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
const activeCardElementIds = computed(() => {
  const nodeIds = new Set<string>();
  const edgeIds = new Set<string>();
  const card = cards.value.find((item) => item.sessionId === activeCardSessionId.value);
  if (!card) return { nodeIds, edgeIds };

  const addedNodeIds = new Set(
    card.nodes.filter((node) => node.changeType === 'added').map((node) => node.id),
  );
  const changedMainNodeIds = new Set([...card.modifiedNodeIds, ...card.deletedNodeIds]);
  for (const node of card.nodes) {
    nodeIds.add(node.changeType === 'added' ? cardDisplayId(card.sessionId, node.id) : node.id);
  }
  for (const nodeId of changedMainNodeIds) nodeIds.add(nodeId);
  for (const edge of card.edges) {
    edgeIds.add(cardDisplayId(card.sessionId, edge.id));
    nodeIds.add(
      addedNodeIds.has(edge.sourceId)
        ? cardDisplayId(card.sessionId, edge.sourceId)
        : edge.sourceId,
    );
    nodeIds.add(
      addedNodeIds.has(edge.targetId)
        ? cardDisplayId(card.sessionId, edge.targetId)
        : edge.targetId,
    );
  }
  for (const edge of graph.value.edges) {
    if (
      edge.sessionId === '' &&
      (changedMainNodeIds.has(edge.sourceId) || changedMainNodeIds.has(edge.targetId))
    ) {
      edgeIds.add(edge.id);
      nodeIds.add(edge.sourceId);
      nodeIds.add(edge.targetId);
    }
  }
  return { nodeIds, edgeIds };
});
const highlightedNodeIds = computed(() =>
  hasSearch.value
    ? searchMatchNodeIds.value
    : selectedNodeId.value
      ? directlyRelatedNodeIds.value
      : activeCardElementIds.value.nodeIds,
);
const highlightedEdgeIds = computed(() =>
  hasSearch.value
    ? new Set<string>()
    : selectedNodeId.value
      ? directlyRelatedEdgeIds.value
      : activeCardElementIds.value.edgeIds,
);
const hasElementHighlight = computed(() =>
  Boolean(hasSearch.value || selectedNodeId.value || activeCardElementIds.value.nodeIds.size),
);
const deletingNode = computed(() =>
  graph.value.nodes.find((node) => node.id === deletingNodeId.value),
);
const deletingEdgeCount = computed(
  () =>
    graph.value.edges.filter(
      (edge) => edge.sourceId === deletingNodeId.value || edge.targetId === deletingNodeId.value,
    ).length,
);
const invalidNodeFiles = computed(() =>
  nodeFiles.value.some(
    (item) =>
      !item.file.trim() ||
      !item.method.trim() ||
      !Number.isInteger(Number(item.startLine)) ||
      Number(item.startLine) < 1 ||
      !Number.isInteger(Number(item.endLine)) ||
      Number(item.endLine) < Number(item.startLine),
  ),
);
const flowNodes = computed({
  get: () =>
    graph.value.nodes.map((node) => ({
      id: node.id,
      type: 'radial',
      label: node.title,
      data: {
        label: node.title,
        content: node.content,
        files: node.files,
        changeType: node.changeType,
        cardLabel: node.cardLabel,
        isTag: isTagNodeId(node.entityId),
      },
      position: activeLayout.value[node.id] ?? { x: 0, y: 0 },
      draggable: false,
      connectable: node.changeType !== 'deleted',
      class: {
        'mind-map-node--root': node.id === rootNodeId,
        'mind-map-node--tag': isTagNodeId(node.entityId),
        'mind-map-node--active': node.id === selectedNodeId.value,
        'mind-map-node--added': node.changeType === 'added',
        'mind-map-node--modified': node.changeType === 'modified',
        'mind-map-node--deleted': node.changeType === 'deleted',
        'mind-map-node--search-match': hasSearch.value && searchMatchNodeIds.value.has(node.id),
        'mind-map-node--related':
          Boolean(selectedNodeId.value) &&
          node.id !== selectedNodeId.value &&
          directlyRelatedNodeIds.value.has(node.id),
        'mind-map-element--highlighted':
          hasElementHighlight.value && highlightedNodeIds.value.has(node.id),
        'mind-map-element--muted':
          hasElementHighlight.value && !highlightedNodeIds.value.has(node.id),
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
      ...radialEdgeHandles(edge, activeLayout.value),
      class: {
        'mind-map-edge--active': directlyRelatedEdgeIds.value.has(edge.id),
        'mind-map-element--highlighted':
          hasElementHighlight.value && highlightedEdgeIds.value.has(edge.id),
        'mind-map-element--muted':
          hasElementHighlight.value && !highlightedEdgeIds.value.has(edge.id),
      },
    })),
  set: () => undefined,
});

function combineMindMaps(
  base: MindMapGraph,
  cardGraphs: MindMapCard[],
  activeCardSessionId: string,
): DisplayMindMapGraph {
  const modifiedNodeIds = new Set(cardGraphs.flatMap((card) => card.modifiedNodeIds));
  const deletedNodeIds = new Set(cardGraphs.flatMap((card) => card.deletedNodeIds));
  const activeCard = cardGraphs.find((card) => card.sessionId === activeCardSessionId);
  const activeDeletedEdgeIds = new Set(activeCard?.deletedEdgeIds ?? []);
  const activeCardLabel = activeCard?.requirement || activeCard?.sessionId || '';
  const activeNodeUpdates = new Map(
    activeCard?.nodes
      .filter((node) => node.changeType === 'modified')
      .map((node) => [node.id, node]) ?? [],
  );
  const nodes: DisplayMindMapNode[] = base.nodes.map((node) => {
    const activeNode = activeNodeUpdates.get(node.id);
    return {
      ...(activeNode ?? node),
      changeType: deletedNodeIds.has(node.id)
        ? 'deleted'
        : modifiedNodeIds.has(node.id)
          ? 'modified'
          : 'unchanged',
      entityId: node.id,
      sessionId: activeNode ? activeCardSessionId : '',
      cardLabel: activeNode ? activeCardLabel : '',
    };
  });
  const edges: DisplayMindMapEdge[] = base.edges
    .filter((edge) => !activeDeletedEdgeIds.has(edge.id))
    .map((edge) => ({
      ...edge,
      entityId: edge.id,
      sessionId: '',
    }));
  const nodeIds = new Set(nodes.map((node) => node.id));

  for (const card of cardGraphs) {
    const addedNodes = card.nodes.filter((node) => node.changeType === 'added');
    const addedNodeIds = new Set(addedNodes.map((node) => node.id));
    const cardLabel = card.requirement || card.sessionId;
    for (const node of addedNodes) {
      const id = cardDisplayId(card.sessionId, node.id);
      nodes.push({
        ...node,
        id,
        changeType: 'added',
        entityId: node.id,
        sessionId: card.sessionId,
        cardLabel,
      });
      nodeIds.add(id);
    }
    for (const edge of card.edges) {
      const sourceId = addedNodeIds.has(edge.sourceId)
        ? cardDisplayId(card.sessionId, edge.sourceId)
        : edge.sourceId;
      const targetId = addedNodeIds.has(edge.targetId)
        ? cardDisplayId(card.sessionId, edge.targetId)
        : edge.targetId;
      if (!nodeIds.has(sourceId) || !nodeIds.has(targetId)) continue;
      edges.push({
        ...edge,
        id: cardDisplayId(card.sessionId, edge.id),
        sourceId,
        targetId,
        entityId: edge.id,
        sessionId: card.sessionId,
      });
    }
  }

  return {
    ...base,
    nodes,
    edges,
    updatedAt: cardGraphs.reduce(
      (latest, card) => (card.updatedAt > latest ? card.updatedAt : latest),
      base.updatedAt,
    ),
  };
}

function cardDisplayId(sessionId: string, entityId: string) {
  return `card:${encodeURIComponent(sessionId)}:${encodeURIComponent(entityId)}`;
}

function isTagNodeId(id: string) {
  return id.startsWith('tag:');
}

onMounted(async () => {
  await Promise.all([loadProjects(), loadDefaultLayout()]);
  await refreshMindMap();
  startMindMapSubscription();
});

watch(searchQuery, () => void runMindMapSearch());

onBeforeUnmount(() => {
  disposed = true;
  graphRequestRevision += 1;
  cardRequestRevision += 1;
  searchRequestRevision += 1;
  stopMindMapSubscription();
  if (taskRefreshTimer) clearTimeout(taskRefreshTimer);
});

async function loadCards() {
  const requestedProjectId = projectId.value;
  const requestRevision = ++cardRequestRevision;
  const result = await listProjectMindMapCards(requestedProjectId);
  if (disposed || requestRevision !== cardRequestRevision || requestedProjectId !== projectId.value)
    return;
  cards.value = result;
  if (!routeCardHighlightApplied) {
    routeCardHighlightApplied = true;
    const requestedCardSessionId = typeof route.query.card === 'string' ? route.query.card : '';
    if (result.some((card) => card.sessionId === requestedCardSessionId)) {
      activeCardSessionId.value = requestedCardSessionId;
    }
  }
  if (
    activeCardSessionId.value &&
    !result.some((card) => card.sessionId === activeCardSessionId.value)
  ) {
    activeCardSessionId.value = '';
  }
  scheduleTaskRefresh();
}

async function loadGraph() {
  if (!projectId.value) return;
  const requestedProjectId = projectId.value;
  const requestRevision = ++graphRequestRevision;
  loading.value = true;
  try {
    const result = await getProjectMindMap(requestedProjectId);
    if (
      disposed ||
      requestRevision !== graphRequestRevision ||
      requestedProjectId !== projectId.value
    ) {
      return;
    }
    mainGraph.value = result;
    clearSelection();
  } finally {
    if (requestRevision === graphRequestRevision) loading.value = false;
  }
}

async function refreshMindMap() {
  refreshPending = true;
  if (refreshPromise) return refreshPromise;
  refreshPromise = (async () => {
    do {
      refreshPending = false;
      await Promise.all([loadCards(), loadGraph()]);
      loadedRevision = [
        mainGraph.value.updatedAt,
        ...cards.value.map((card) => card.updatedAt),
      ].reduce((latest, value) => (value > latest ? value : latest), '');
    } while (refreshPending);
    if (hasSearch.value) await runMindMapSearch();
    fitGraph();
  })();
  try {
    await refreshPromise;
  } finally {
    refreshPromise = null;
  }
}

function scheduleTaskRefresh() {
  if (taskRefreshTimer) clearTimeout(taskRefreshTimer);
  taskRefreshTimer = null;
  if (!cards.value.some((card) => card.taskStatus === 'queued' || card.taskStatus === 'running'))
    return;
  taskRefreshTimer = setTimeout(() => {
    taskRefreshTimer = null;
    void loadCards();
  }, 2000);
}

function startMindMapSubscription() {
  if (disposed || !projectId.value) return;
  const currentRevision = ++subscriptionRevision;
  const requestedProjectId = projectId.value;
  if (subscriptionReconnectTimer) clearTimeout(subscriptionReconnectTimer);
  subscriptionReconnectTimer = null;
  mindMapSubscription?.unsubscribe();
  mindMapSubscription = subscribeMindMapUpdates(requestedProjectId, '', {
    onData: (update) => {
      if (currentRevision === subscriptionRevision && update.updatedAt !== loadedRevision)
        void refreshMindMap();
    },
    onError: () => scheduleSubscriptionReconnect(currentRevision),
    onClose: () => scheduleSubscriptionReconnect(currentRevision),
  });
}

function scheduleSubscriptionReconnect(currentRevision: number) {
  if (disposed || currentRevision !== subscriptionRevision || subscriptionReconnectTimer) return;
  subscriptionReconnectTimer = setTimeout(() => {
    subscriptionReconnectTimer = null;
    if (currentRevision === subscriptionRevision) startMindMapSubscription();
  }, 1500);
}

function stopMindMapSubscription() {
  subscriptionRevision += 1;
  mindMapSubscription?.unsubscribe();
  mindMapSubscription = null;
  if (subscriptionReconnectTimer) clearTimeout(subscriptionReconnectTimer);
  subscriptionReconnectTimer = null;
}

async function applyOperations(operations: MindMapOperation[], sessionId = '') {
  const requestedProjectId = projectId.value;
  await updateProjectMindMap({
    projectId: requestedProjectId,
    ...(sessionId ? { sessionId } : {}),
    operations,
  });
  if (requestedProjectId !== projectId.value) return;
  await refreshMindMap();
}

function fitGraph() {
  requestAnimationFrame(() => void fitView({ padding: 0.2 }));
}

async function loadDefaultLayout() {
  try {
    activeLayoutAlgorithm.value = (await getGeneralSettings()).mindMapLayout;
  } catch {
    activeLayoutAlgorithm.value = 'radial';
  }
}

function selectTemporaryLayout(layout: MindMapLayout) {
  if (activeLayoutAlgorithm.value === layout) return;
  activeLayoutAlgorithm.value = layout;
  clearSelection();
  fitGraph();
}

async function runMindMapSearch() {
  const query = searchQuery.value?.trim() ?? '';
  const requestedProjectId = projectId.value;
  const requestRevision = ++searchRequestRevision;
  if (!query) {
    searchMatchNodeIds.value = new Set();
    searchLoading.value = false;
    return;
  }
  searchLoading.value = true;
  try {
    const result = await searchProjectMindMap(requestedProjectId, query);
    if (
      disposed ||
      requestRevision !== searchRequestRevision ||
      requestedProjectId !== projectId.value ||
      query !== searchQuery.value?.trim()
    ) {
      return;
    }
    searchMatchNodeIds.value = new Set(result.matches.map(searchDisplayNodeId));
  } catch {
    if (requestRevision === searchRequestRevision) {
      searchMatchNodeIds.value = new Set();
      $q.notify({ type: 'negative', message: '搜索思维图失败' });
    }
  } finally {
    if (requestRevision === searchRequestRevision) searchLoading.value = false;
  }
}

function searchDisplayNodeId(match: { nodeId: string; sessionId?: string | null }) {
  if (!match.sessionId) return match.nodeId;
  const node = cards.value
    .find((card) => card.sessionId === match.sessionId)
    ?.nodes.find((item) => item.id === match.nodeId);
  return node?.changeType === 'modified'
    ? match.nodeId
    : cardDisplayId(match.sessionId, match.nodeId);
}

function selectNode({ node }: NodeMouseEvent) {
  selectedNodeId.value = node.id;
  if ($q.platform.is.mobile) showNodeInfo(node.id);
}

function toggleCardHighlight(sessionId: string) {
  activeCardSessionId.value = activeCardSessionId.value === sessionId ? '' : sessionId;
  clearSelection();
}

function showNodeInfo(nodeId: string) {
  infoNodeId.value = nodeId;
}

function syncNodeInfo(nodeId: string, open: boolean) {
  if (open) {
    infoNodeId.value = nodeId;
  } else if (infoNodeId.value === nodeId) {
    closeNodeInfo();
  }
}

function closeNodeInfo() {
  infoNodeId.value = '';
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
  nodeFiles.value = [];
  editDialog.value = true;
}

function openNodeEditor(nodeId: string) {
  const node = graph.value.nodes.find((item) => item.id === nodeId);
  if (!node || node.id === rootNodeId || node.changeType === 'deleted') return;
  editingNodeId.value = node.id;
  nodeTitle.value = node.title;
  nodeContent.value = node.content;
  nodeFiles.value = node.files.map((item) => ({ ...item }));
  editDialog.value = true;
}

function addNodeFile() {
  nodeFiles.value.push({ file: '', method: '', startLine: 1, endLine: 1 });
}

function openDeleteDialog(nodeId: string) {
  const node = graph.value.nodes.find((item) => item.id === nodeId);
  if (!node || node.id === rootNodeId || node.changeType === 'deleted') return;
  deletingNodeId.value = nodeId;
  deleteDialog.value = true;
}

async function saveNode() {
  const title = nodeTitle.value.trim();
  if (!title || editingNodeId.value === rootNodeId) return;
  saving.value = true;
  try {
    const editingNode = graph.value.nodes.find((node) => node.id === editingNodeId.value);
    const nodeId = editingNode?.entityId || crypto.randomUUID();
    const operation: MindMapOperation = {
      kind: 'upsert_node',
      id: nodeId,
      title,
      content: nodeContent.value,
      files: nodeFiles.value.map((item) => ({
        file: item.file.trim(),
        method: item.method.trim(),
        startLine: Number(item.startLine),
        endLine: Number(item.endLine),
      })),
    };
    await applyOperations([operation], editingNode?.sessionId);
    selectedNodeId.value = editingNode?.id || nodeId;
    editDialog.value = false;
  } finally {
    saving.value = false;
  }
}

async function deleteNode() {
  if (
    !deletingNode.value ||
    deletingNode.value.id === rootNodeId ||
    deletingNode.value.changeType === 'deleted'
  )
    return;
  const nodeId = deletingNode.value.entityId;
  const sessionId = deletingNode.value.sessionId;
  saving.value = true;
  try {
    await applyOperations([{ kind: 'delete_node', id: nodeId }], sessionId);
    deleteDialog.value = false;
    deletingNodeId.value = '';
    clearSelection();
  } finally {
    saving.value = false;
  }
}

async function createEdge(connection: Connection) {
  if (!connection.source || !connection.target || connection.source === connection.target) return;
  const source = graph.value.nodes.find((node) => node.id === connection.source);
  const target = graph.value.nodes.find((node) => node.id === connection.target);
  if (!source || !target || source.changeType === 'deleted' || target.changeType === 'deleted')
    return;
  const cardSessionIds = new Set([source.sessionId, target.sessionId].filter(Boolean));
  if (cardSessionIds.size > 1) {
    $q.notify({ type: 'warning', message: '不同卡片的节点不能直接建立关系' });
    return;
  }
  const sessionId = [...cardSessionIds][0] ?? '';
  await applyOperations(
    [
      {
        kind: 'upsert_edge',
        id: crypto.randomUUID(),
        sourceId: source.entityId,
        targetId: target.entityId,
        label: '',
      },
    ],
    sessionId,
  );
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
  closeNodeInfo();
}

function taskStatusLabel(status: string) {
  return (
    { queued: '排队中', running: '整理中', failed: '失败', completed: '已完成' }[status] ?? status
  );
}

function taskStatusColor(status: string) {
  return (
    { queued: 'grey-7', running: 'primary', failed: 'negative', completed: 'positive' }[status] ??
    'grey-7'
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

.mind-map-card-strip {
  display: flex;
  flex: 0 0 auto;
  gap: 10px;
  padding: 10px 12px;
  overflow-x: auto;
  border-bottom: 1px solid var(--ac-border);
  background: var(--ac-page-bg);
  scrollbar-width: thin;
}

.mind-map-card {
  width: 240px;
  min-width: 240px;
  border-radius: 14px;
  background: var(--ac-surface);
  box-shadow: var(--ac-shadow-card);
  cursor: pointer;
  transition:
    filter 160ms ease,
    transform 160ms ease;
}

.mind-map-card:hover,
.mind-map-card:focus-visible {
  filter: brightness(1.04);
}

.mind-map-card:focus-visible {
  outline: 2px solid currentcolor;
  outline-offset: -2px;
}

.mind-map-card--active {
  filter: brightness(1.1);
  transform: translateY(-1px);
}

.mind-map-card__body {
  position: relative;
  min-height: 82px;
  padding: 10px 36px 10px 12px;
}

.mind-map-card__badges {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--ac-text-muted);
  font-size: 12px;
  font-weight: 600;
}

.mind-map-card__title {
  display: -webkit-box;
  margin-top: 7px;
  overflow: hidden;
  font-size: 13px;
  font-weight: 650;
  line-height: 1.35;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}

.mind-map-card__error {
  margin-top: 4px;
  overflow: hidden;
  color: var(--q-negative);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mind-map-card__retry {
  position: absolute;
  top: 6px;
  right: 4px;
}

.mind-map-change-legend {
  position: absolute;
  z-index: 5;
  bottom: 12px;
  left: 16px;
  display: flex;
  max-width: calc(100% - 32px);
  flex-wrap: wrap;
  gap: 8px 14px;
  padding: 7px 10px;
  border: 1px solid var(--ac-border);
  border-radius: 16px;
  background: color-mix(in srgb, var(--ac-surface) 92%, transparent);
  box-shadow: var(--ac-shadow-card);
  color: var(--ac-text-muted);
  font-size: 12px;
}

.mind-map-change-legend > span {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}

.mind-map-change-legend__dot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
}

.mind-map-change-legend__dot--added {
  background: var(--q-positive);
}

.mind-map-change-legend__dot--modified {
  background: var(--q-warning);
}

.mind-map-change-legend__dot--deleted {
  background: var(--q-negative);
}

.mind-map-canvas {
  position: relative;
  min-height: 0;
  flex: 1 1 auto;
  overflow: hidden;
  border-radius: 0;
  touch-action: none;
}

.mind-map-toolbar-search {
  width: min(320px, 30vw);
  min-width: 100px;
  max-width: 360px;
  flex: 1 1 320px;
  border-radius: 4px;
  background: var(--ac-surface);
}

.mind-map-toolbar-search__count {
  color: var(--ac-text-muted);
  font-size: 12px;
  white-space: nowrap;
}

.mind-map-canvas :deep(.vue-flow) {
  position: absolute;
  inset: 0;
  width: auto;
  height: auto;
}

.mind-map-canvas :deep(.vue-flow__background) {
  pointer-events: none;
}

.mind-map-canvas :deep(.vue-flow__viewport),
.mind-map-canvas :deep(.vue-flow__pane) {
  touch-action: none;
}

.mind-map-node-content {
  display: flex;
  width: 172px;
  min-height: 48px;
  flex-direction: column;
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

.mind-map-node-content__title {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mind-map-node-content__tag {
  margin-top: 2px;
  color: var(--q-primary);
  font-size: 9px;
  font-weight: 700;
  letter-spacing: 0.08em;
}

.mind-map-node-content__card {
  display: block;
  max-width: 100%;
  margin-top: 2px;
  overflow: hidden;
  color: var(--ac-text-muted);
  font-size: 10px;
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mind-map-node-info {
  width: min(360px, calc(100vw - 32px));
  max-height: min(420px, calc(100vh - 32px));
  overflow-x: hidden;
}

.mind-map-node-info__title {
  min-width: 0;
  padding-top: 6px;
  font-size: 15px;
  font-weight: 700;
  overflow-wrap: anywhere;
}

.mind-map-node-info__content {
  overflow: auto;
  color: var(--ac-text-muted);
  font-size: 13px;
  line-height: 1.6;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

.mind-map-node-info__files {
  max-height: 200px;
  overflow: auto;
}

.mind-map-node-info__files :deep(.q-item__section) {
  min-width: 0;
}

.mind-map-node-info__files :deep(.q-item__label) {
  overflow-wrap: anywhere;
}

.mind-map-canvas :deep(.vue-flow__node),
.mind-map-canvas :deep(.vue-flow__edge) {
  transition:
    filter 160ms ease,
    opacity 160ms ease;
}

.mind-map-canvas :deep(.mind-map-node--root .mind-map-node-content) {
  border-color: var(--q-primary);
  background: var(--q-primary);
  color: white;
}

.mind-map-canvas :deep(.mind-map-node--tag .mind-map-node-content) {
  border-color: color-mix(in srgb, var(--q-primary) 58%, var(--ac-border));
  background: color-mix(in srgb, var(--q-primary) 9%, var(--ac-surface));
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

.mind-map-canvas :deep(.mind-map-node--search-match .mind-map-node-content) {
  box-shadow:
    0 0 0 3px color-mix(in srgb, var(--q-primary) 32%, transparent),
    0 0 18px color-mix(in srgb, var(--q-primary) 24%, transparent),
    var(--ac-shadow-card);
}

.mind-map-canvas :deep(.mind-map-node--added .mind-map-node-content) {
  border-color: var(--q-positive);
  background: color-mix(in srgb, var(--q-positive) 12%, var(--ac-surface));
}

.mind-map-canvas :deep(.mind-map-node--modified .mind-map-node-content) {
  border-color: var(--q-warning);
  background: color-mix(in srgb, var(--q-warning) 14%, var(--ac-surface));
}

.mind-map-canvas :deep(.mind-map-node--deleted .mind-map-node-content) {
  border-color: var(--q-negative);
  border-style: dashed;
  background: color-mix(in srgb, var(--q-negative) 10%, var(--ac-surface));
  color: var(--q-negative);
  text-decoration: line-through;
}

.mind-map-canvas :deep(.mind-map-node--deleted .vue-flow__handle) {
  display: none;
}

.mind-map-canvas :deep(.mind-map-element--highlighted) {
  filter: brightness(1.14);
  opacity: 1;
}

.mind-map-canvas :deep(.mind-map-element--muted) {
  filter: brightness(0.58);
  opacity: 0.2;
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
  width: min(760px, calc(100vw - 32px));
}

.mind-map-file-editor {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(0, 1.5fr) 92px 92px auto;
  gap: 8px;
  align-items: center;
}

.mind-map-node-menu {
  min-width: 160px;
}

@media (max-width: 699px) {
  .mind-map-toolbar-search {
    width: auto;
    min-width: 72px;
  }

  .mind-map-file-editor {
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto;
  }

  .mind-map-file-editor :deep(.q-field:nth-child(-n + 2)) {
    grid-column: span 3;
  }

  .mind-map-card-strip {
    gap: 8px;
    padding: 8px;
  }

  .mind-map-card {
    width: min(76vw, 250px);
    min-width: min(76vw, 250px);
  }

  .mind-map-change-legend {
    bottom: 8px;
    left: 8px;
    max-width: calc(100% - 16px);
  }
}
</style>
