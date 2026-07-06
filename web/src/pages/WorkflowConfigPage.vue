<template>
  <q-page class="page-shell">
    <div class="page-heading">
      <div>
        <div class="text-h5 text-weight-bold">流程配置</div>
        <div class="text-body2 text-muted">
          {{ projectName }} · 拖动节点调整视图，点击右侧端口后点击目标左侧端口创建连线
        </div>
      </div>
      <div class="row items-center q-gutter-sm">
        <q-chip v-if="definitionId" dense outline color="primary">v{{ version }}</q-chip>
        <q-btn
          unelevated
          color="positive"
          text-color="dark"
          icon="save"
          label="保存为默认流程"
          no-caps
          :loading="saving"
          @click="saveDefinition"
        />
      </div>
    </div>
    <div class="workflow-layout">
      <q-card flat bordered class="workflow-list">
        <q-card-section class="row items-center">
          <div class="text-subtitle1 text-weight-bold">节点</div>
          <q-space />
          <q-btn flat round dense icon="add" color="primary" aria-label="新增节点" @click="addNode">
            <q-tooltip>新增节点</q-tooltip>
          </q-btn>
        </q-card-section>
        <q-list separator>
          <q-item
            v-for="node in graph.nodes"
            :key="node.id"
            clickable
            :active="node.id === selectedNodeId"
            @click="selectNode(node.id)"
          >
            <q-item-section avatar>
              <q-icon :name="nodeIcon(node.type)" color="primary" />
            </q-item-section>
            <q-item-section>
              <q-item-label>{{ node.title || node.id }}</q-item-label>
              <q-item-label caption>{{ nodeCaption(node.id) }}</q-item-label>
            </q-item-section>
          </q-item>
        </q-list>
      </q-card>

      <q-card flat bordered class="workflow-canvas">
        <q-card-section class="workflow-canvas__header">
          <q-input v-model="workflowName" dense outlined label="流程名称" />
          <q-chip v-if="connectingFrom" dense color="primary" text-color="white">
            连接自 {{ connectingFrom }}
          </q-chip>
        </q-card-section>
        <div
          ref="canvasRef"
          class="workflow-canvas-board"
          @pointermove="dragMove"
          @pointerup="endDrag"
          @pointerleave="endDrag"
        >
          <svg class="workflow-edges" aria-hidden="true">
            <defs>
              <marker id="workflow-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
                <path d="M 0 0 L 10 5 L 0 10 z" />
              </marker>
            </defs>
            <path
              v-for="(edge, index) in graph.edges"
              :key="`${edge.from}-${edge.to}-${index}`"
              class="workflow-edge"
              :d="edgePath(edge)"
            />
          </svg>

          <button
            v-for="(node, index) in graph.nodes"
            :key="node.id"
            type="button"
            class="workflow-node"
            :class="{ 'workflow-node--active': node.id === selectedNodeId }"
            :style="nodeStyle(node.id, index)"
            @click="selectNode(node.id)"
            @pointerdown="startDrag($event, node.id, index)"
          >
            <span
              class="workflow-port workflow-port--input"
              title="连接到此节点"
              @click.stop="finishConnect(node.id)"
              @pointerdown.stop
            />
            <q-icon :name="nodeIcon(node.type)" color="primary" />
            <span class="workflow-node__body">
              <span class="text-weight-medium">{{ node.title || node.id }}</span>
              <span class="text-caption text-muted">{{ node.type }} · retry {{ node.retry.maxAttempts }}</span>
            </span>
            <span
              class="workflow-port workflow-port--output"
              title="从此节点连线"
              :class="{ 'workflow-port--connecting': connectingFrom === node.id }"
              @click.stop="startConnect(node.id)"
              @pointerdown.stop
            />
          </button>
        </div>
      </q-card>

      <q-card flat bordered class="workflow-editor">
        <q-card-section>
          <div class="text-subtitle1 text-weight-bold">节点配置</div>
        </q-card-section>
        <q-card-section class="q-gutter-md">
          <q-input v-model="nodeId" dense outlined label="节点 ID" />
          <q-select v-model="nodeType" dense outlined label="类型" :options="nodeTypeOptions" />
          <q-input v-model="nodeTitle" dense outlined label="标题" />
          <q-input v-model="nodePrompt" autogrow outlined type="textarea" label="提示词" />
          <q-input v-model.number="retry" dense outlined type="number" label="失败重试次数" min="0" />
          <q-toggle v-model="requiresApproval" label="运行前人工审批" />
          <q-select
            v-if="nodeType === 'merge'"
            v-model="mergeStrategy"
            dense
            outlined
            label="合并策略"
            :options="mergeStrategyOptions"
          />
          <div class="row q-gutter-sm">
            <q-btn outline color="primary" icon="check" label="应用节点" no-caps @click="applyNodeEdit" />
            <q-btn
              flat
              round
              color="negative"
              icon="delete"
              aria-label="删除节点"
              :disable="graph.nodes.length <= 1"
              @click="deleteSelectedNode"
            >
              <q-tooltip>删除节点</q-tooltip>
            </q-btn>
          </div>
          <q-separator />
          <div class="workflow-edge-list">
            <div class="text-subtitle2 text-weight-bold">连线</div>
            <q-list v-if="graph.edges.length > 0" dense separator bordered>
              <q-item v-for="(edge, index) in graph.edges" :key="`${edge.from}-${edge.to}-${index}`">
                <q-item-section>
                  <q-item-label>{{ edge.from }} → {{ edge.to }}</q-item-label>
                  <q-item-label caption>priority {{ edge.priority }}</q-item-label>
                </q-item-section>
                <q-item-section side>
                  <q-btn flat round dense color="negative" icon="close" aria-label="删除连线" @click="deleteEdge(index)">
                    <q-tooltip>删除连线</q-tooltip>
                  </q-btn>
                </q-item-section>
              </q-item>
            </q-list>
            <div v-else class="text-caption text-muted">暂无连线</div>
          </div>
        </q-card-section>
      </q-card>
    </div>

    <q-inner-loading :showing="loading">
      <q-spinner color="primary" size="32px" />
    </q-inner-loading>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { useQuasar } from 'quasar';
import { useRoute, useRouter } from 'vue-router';

import { useProjects } from '@/composables/useProjects';
import {
  getWorkflowDefinition,
  saveWorkflowDefinition as saveWorkflow,
  setDefaultWorkflow,
  type WorkflowEdge,
  type WorkflowGraph,
  type WorkflowNode,
} from '@/services/workflows';

interface NodePosition {
  x: number;
  y: number;
}

const route = useRoute();
const router = useRouter();
const $q = useQuasar();
const { projects, loadProjects } = useProjects();

const loading = ref(false);
const saving = ref(false);
const definitionId = ref('');
const version = ref(1);
const workflowName = ref('默认流程');
const selectedNodeId = ref('');
const nodeId = ref('');
const nodeType = ref('codex');
const nodeTitle = ref('');
const nodePrompt = ref('');
const retry = ref(0);
const requiresApproval = ref(false);
const mergeStrategy = ref('merge');
const connectingFrom = ref('');
const canvasRef = ref<HTMLElement | null>(null);
const nodePositions = reactive<Record<string, NodePosition>>({});
const dragState = ref<{ id: string; offsetX: number; offsetY: number } | null>(null);
const graph = reactive<WorkflowGraph>(defaultGraph());

const nodeTypeOptions = ['codex', 'approval', 'merge'];
const mergeStrategyOptions = ['merge', 'rebase'];
const nodeWidth = 232;
const nodeHeight = 78;

const projectId = computed(() => String(route.params.projectId ?? ''));
const project = computed(() => projects.value.find((item) => item.id === projectId.value));
const projectName = computed(() => project.value?.name ?? projectId.value);

onMounted(async () => {
  loading.value = true;
  try {
    await loadProjects();
    const workflowId = String(route.query.workflowId ?? project.value?.defaultWorkflowId ?? '');
    if (workflowId) {
      await loadWorkflow(workflowId);
    } else {
      setGraph(defaultGraph());
    }
  } catch (err) {
    notifyError(err, '加载流程配置失败');
  } finally {
    loading.value = false;
  }
});

async function loadWorkflow(id: string) {
  const definition = await getWorkflowDefinition(id);
  if (!definition) return;
  definitionId.value = definition.id;
  version.value = definition.version;
  workflowName.value = definition.name;
  setGraph(definition.graph);
}

function setGraph(next: WorkflowGraph) {
  graph.nodes.splice(0, graph.nodes.length, ...next.nodes.map(normalizeNode));
  graph.edges.splice(0, graph.edges.length, ...next.edges.map(normalizeEdge));
  selectedNodeId.value = graph.nodes[0]?.id ?? '';
  layoutMissingNodes();
  loadSelectedNode();
}

function selectNode(id: string) {
  if (dragState.value) return;
  applyNodeEdit();
  selectedNodeId.value = id;
  loadSelectedNode();
}

function loadSelectedNode() {
  const node = currentNode();
  if (!node) return;
  nodeId.value = node.id;
  nodeType.value = node.type;
  nodeTitle.value = node.title;
  nodePrompt.value = node.prompt;
  retry.value = node.retry.maxAttempts;
  requiresApproval.value = node.approval.beforeRun;
  mergeStrategy.value = node.merge?.strategy ?? 'merge';
}

function applyNodeEdit() {
  const node = currentNode();
  if (!node) return;
  const oldId = node.id;
  const nextId = normalizeID(nodeId.value);
  node.id = nextId;
  node.type = nodeType.value;
  node.title = nodeTitle.value.trim() || nextId;
  node.prompt = nodePrompt.value.trim();
  node.retry.maxAttempts = Math.max(0, Number(retry.value) || 0);
  node.approval.beforeRun = requiresApproval.value || nodeType.value === 'approval';
  node.approval.afterRun = false;
  node.merge = nodeType.value === 'merge' ? { strategy: mergeStrategy.value } : null;
  if (oldId !== nextId) {
    graph.edges.forEach((edge) => {
      if (edge.from === oldId) edge.from = nextId;
      if (edge.to === oldId) edge.to = nextId;
    });
    nodePositions[nextId] = nodePositions[oldId] ?? defaultPosition(graph.nodes.length - 1);
    delete nodePositions[oldId];
    selectedNodeId.value = nextId;
  }
}

function addNode() {
  applyNodeEdit();
  const id = uniqueNodeID('node');
  graph.nodes.push(normalizeNode({ id, type: 'codex', title: '新节点', prompt: '', retry: { maxAttempts: 0 } }));
  nodePositions[id] = defaultPosition(graph.nodes.length - 1);
  selectedNodeId.value = id;
  loadSelectedNode();
}

function deleteSelectedNode() {
  const id = selectedNodeId.value;
  const index = graph.nodes.findIndex((node) => node.id === id);
  if (index < 0 || graph.nodes.length <= 1) return;
  graph.nodes.splice(index, 1);
  graph.edges.splice(
    0,
    graph.edges.length,
    ...graph.edges.filter((edge) => edge.from !== id && edge.to !== id),
  );
  delete nodePositions[id];
  selectedNodeId.value = graph.nodes[Math.max(0, index - 1)]?.id ?? '';
  loadSelectedNode();
}

async function saveDefinition() {
  applyNodeEdit();
  graph.edges.splice(0, graph.edges.length, ...graph.edges.map(normalizeEdge));

  saving.value = true;
  try {
    const definition = await saveWorkflow({
      projectId: projectId.value,
      name: workflowName.value.trim() || '默认流程',
      graph: { nodes: [...graph.nodes], edges: [...graph.edges] },
    });
    await setDefaultWorkflow({ projectId: projectId.value, workflowId: definition.id });
    definitionId.value = definition.id;
    version.value = definition.version;
    setGraph(definition.graph);
    await loadProjects();
    await router.replace({
      name: 'workflow-config',
      params: { projectId: projectId.value },
      query: { workflowId: definition.id },
    });
    $q.notify({ type: 'positive', message: '流程已保存为项目默认流程' });
  } catch (err) {
    notifyError(err, '保存流程配置失败');
  } finally {
    saving.value = false;
  }
}

function startConnect(id: string) {
  connectingFrom.value = connectingFrom.value === id ? '' : id;
}

function finishConnect(targetId: string) {
  if (!connectingFrom.value || connectingFrom.value === targetId) return;
  const exists = graph.edges.some((edge) => edge.from === connectingFrom.value && edge.to === targetId);
  if (!exists) {
    graph.edges.push(
      normalizeEdge({
        from: connectingFrom.value,
        to: targetId,
        priority: graph.edges.filter((edge) => edge.from === connectingFrom.value).length,
      }),
    );
  }
  connectingFrom.value = '';
}

function deleteEdge(index: number) {
  graph.edges.splice(index, 1);
}

function startDrag(event: PointerEvent, id: string, index: number) {
  const canvas = canvasRef.value;
  if (!canvas) return;
  const rect = canvas.getBoundingClientRect();
  const position = nodePosition(id, index);
  dragState.value = {
    id,
    offsetX: event.clientX - rect.left - position.x,
    offsetY: event.clientY - rect.top - position.y,
  };
  selectedNodeId.value = id;
  loadSelectedNode();
  (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
}

function dragMove(event: PointerEvent) {
  if (!dragState.value || !canvasRef.value) return;
  const rect = canvasRef.value.getBoundingClientRect();
  const x = event.clientX - rect.left - dragState.value.offsetX;
  const y = event.clientY - rect.top - dragState.value.offsetY;
  nodePositions[dragState.value.id] = {
    x: clamp(x, 16, Math.max(16, rect.width - nodeWidth - 16)),
    y: clamp(y, 16, Math.max(16, rect.height - nodeHeight - 16)),
  };
}

function endDrag() {
  dragState.value = null;
}

function currentNode() {
  return graph.nodes.find((node) => node.id === selectedNodeId.value);
}

function nodeCaption(id: string) {
  const out = graph.edges.filter((edge) => edge.from === id).length;
  const incoming = graph.edges.filter((edge) => edge.to === id).length;
  return `${incoming} 入 / ${out} 出`;
}

function nodeIcon(type: string) {
  if (type === 'approval') return 'approval';
  if (type === 'merge') return 'merge_type';
  return 'terminal';
}

function nodePosition(id: string, index: number) {
  if (!nodePositions[id]) {
    nodePositions[id] = defaultPosition(index);
  }
  return nodePositions[id];
}

function nodeStyle(id: string, index: number) {
  const position = nodePosition(id, index);
  return {
    transform: `translate(${position.x}px, ${position.y}px)`,
  };
}

function edgePath(edge: WorkflowEdge) {
  const fromIndex = graph.nodes.findIndex((node) => node.id === edge.from);
  const toIndex = graph.nodes.findIndex((node) => node.id === edge.to);
  if (fromIndex < 0 || toIndex < 0) return '';
  const from = nodePosition(edge.from, fromIndex);
  const to = nodePosition(edge.to, toIndex);
  const x1 = from.x + nodeWidth;
  const y1 = from.y + nodeHeight / 2;
  const x2 = to.x;
  const y2 = to.y + nodeHeight / 2;
  const curve = Math.max(60, Math.abs(x2 - x1) / 2);
  return `M ${x1} ${y1} C ${x1 + curve} ${y1}, ${x2 - curve} ${y2}, ${x2} ${y2}`;
}

function layoutMissingNodes() {
  graph.nodes.forEach((node, index) => {
    if (!nodePositions[node.id]) {
      nodePositions[node.id] = defaultPosition(index);
    }
  });
}

function defaultPosition(index: number): NodePosition {
  return {
    x: 32 + (index % 2) * 300,
    y: 32 + Math.floor(index / 2) * 140,
  };
}

function defaultGraph(): WorkflowGraph {
  return {
    nodes: [
      {
        id: 'implement',
        type: 'codex',
        title: '实现',
        prompt: '',
        approval: { beforeRun: false, afterRun: false },
        retry: { maxAttempts: 1 },
        merge: null,
      },
    ],
    edges: [],
  };
}

function normalizeNode(node: Partial<WorkflowNode> & { id: string }): WorkflowNode {
  return {
    id: normalizeID(node.id),
    type: node.type || 'codex',
    title: node.title || node.id,
    prompt: node.prompt || '',
    approval: {
      beforeRun: Boolean(node.approval?.beforeRun),
      afterRun: Boolean(node.approval?.afterRun),
    },
    retry: { maxAttempts: Math.max(0, Number(node.retry?.maxAttempts ?? 0)) },
    merge: node.merge ? { strategy: node.merge.strategy === 'rebase' ? 'rebase' : 'merge' } : null,
  };
}

function normalizeEdge(edge: Partial<WorkflowEdge>): WorkflowEdge {
  return {
    from: String(edge.from ?? ''),
    to: String(edge.to ?? ''),
    priority: Number(edge.priority ?? 0),
    condition: normalizeCondition(edge.condition),
  };
}

function normalizeCondition(condition: Partial<WorkflowEdge['condition']> | undefined) {
  return {
    field: String(condition?.field ?? ''),
    op: String(condition?.op ?? ''),
    value: condition?.value,
    all: condition?.all ?? [],
    any: condition?.any ?? [],
    not: condition?.not ?? null,
  };
}

function normalizeID(value: string) {
  const id = value.trim().replace(/\s+/g, '-');
  return id || uniqueNodeID('node');
}

function uniqueNodeID(prefix: string) {
  let index = graph.nodes.length + 1;
  let id = `${prefix}-${index}`;
  while (graph.nodes.some((node) => node.id === id)) {
    index += 1;
    id = `${prefix}-${index}`;
  }
  return id;
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
}

function notifyError(err: unknown, fallback: string) {
  if (wasNotified(err)) return;
  $q.notify({
    type: 'negative',
    icon: 'error',
    position: 'top-right',
    message: err instanceof Error ? err.message || fallback : fallback,
    timeout: 5000,
    actions: [{ icon: 'close', color: 'white', round: true }],
  });
}

function wasNotified(err: unknown) {
  return Boolean(err && typeof err === 'object' && '__anycodeNotified' in err);
}
</script>
