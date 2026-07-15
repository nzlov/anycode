<template>
  <q-page class="page-shell">
    <div class="page-heading">
      <div class="text-h5 text-weight-bold">流程配置</div>
      <div class="row items-center q-gutter-sm">
        <q-chip v-if="definitionId" dense outline color="primary">v{{ version }}</q-chip>
        <q-btn flat round color="primary" icon="content_copy" aria-label="复制流程配置" @click="copyWorkflowConfig">
          <q-tooltip>复制流程配置</q-tooltip>
        </q-btn>
        <q-btn flat round color="primary" icon="content_paste" aria-label="从剪贴板导入流程配置" @click="importWorkflowConfig">
          <q-tooltip>从剪贴板导入流程配置</q-tooltip>
        </q-btn>
        <q-btn
          unelevated
          color="positive"
          text-color="dark"
          icon="save"
          label="保存"
          no-caps
          :loading="saving"
          @click="saveDefinition"
        />
      </div>
    </div>
    <div class="workflow-layout">
      <q-card flat bordered class="workflow-list">
        <q-card-section class="row items-center">
          <div class="text-subtitle1 text-weight-bold">节点类型</div>
        </q-card-section>
        <q-list separator>
          <q-item
            v-for="type in nodePalette"
            :key="type.value"
            clickable
            draggable="true"
            @dragstart="dragNodeType($event, type.value)"
          >
            <q-item-section avatar>
              <q-icon :name="nodeIcon(type.value)" color="primary" />
            </q-item-section>
            <q-item-section>
              <q-item-label>{{ type.label }}</q-item-label>
              <q-item-label caption>{{ type.caption }}</q-item-label>
            </q-item-section>
          </q-item>
        </q-list>
      </q-card>

      <q-card flat bordered class="workflow-canvas">
        <q-card-section class="workflow-canvas__header">
          <q-input v-model="workflowName" dense outlined label="流程名称" />
          <q-chip dense outline color="primary">滚轮缩放 · 左键框选 · Ctrl 多选</q-chip>
        </q-card-section>
        <div
          class="workflow-canvas-board"
          @dragover.prevent
          @drop="dropNodeType"
        >
          <VueFlow
            id="workflow-config-flow"
            v-model:nodes="flowNodes"
            v-model:edges="flowEdges"
            class="workflow-flow"
            :min-zoom="0.2"
            :max-zoom="2"
            :default-viewport="{ x: 32, y: 32, zoom: 1 }"
            :nodes-draggable="true"
            :nodes-connectable="true"
            :elements-selectable="true"
            :multi-selection-key-code="flowInteraction.multiSelectionKeyCode"
            :selection-key-code="flowInteraction.selectionKeyCode"
            :pan-on-drag="flowInteraction.panOnDrag"
            @node-click="handleFlowNodeClick"
            @edge-click="handleFlowEdgeClick"
            @node-drag-stop="handleFlowNodeDragStop"
            @connect="handleFlowConnect"
          >
            <Background pattern-color="var(--ac-border)" :gap="24" />
            <Controls position="top-right" />
            <template #node-workflow="{ id, data, selected }">
              <div class="workflow-node" :class="{ 'workflow-node--active': selected || id === selectedNodeId }">
                <Handle type="target" :position="Position.Left" class="workflow-flow-handle" />
                <q-icon :name="nodeIcon(data.nodeType)" color="primary" />
                <span class="workflow-node__body">
                  <span class="text-weight-medium">{{ data.title || id }}</span>
                  <span class="text-caption text-muted">{{ data.nodeType }} · retry {{ data.retry }}</span>
                </span>
                <Handle type="source" :position="Position.Right" class="workflow-flow-handle" />
              </div>
            </template>
          </VueFlow>
        </div>
      </q-card>

      <q-card flat bordered class="workflow-editor">
        <q-card-section>
          <div class="text-subtitle1 text-weight-bold">节点配置</div>
        </q-card-section>
        <q-card-section v-if="selectedNode" class="q-gutter-md">
          <q-input v-model="nodeId" dense outlined label="节点 ID" />
          <q-select v-model="nodeType" dense outlined label="类型" :options="nodeTypeOptions" />
          <q-input v-model="nodeTitle" dense outlined label="标题" />
          <q-input v-model="nodePrompt" autogrow outlined type="textarea" :label="nodeType === 'expr' ? 'expr 脚本' : '提示词'" />
          <div class="workflow-output-fields">
            <div class="row items-center">
              <div class="text-subtitle2 text-weight-bold">输出字段</div>
              <q-space />
              <q-btn flat round dense icon="add" color="primary" aria-label="新增输出字段" @click="addOutputField">
                <q-tooltip>新增输出字段</q-tooltip>
              </q-btn>
            </div>
            <div v-if="outputFields.length > 0" class="q-gutter-sm">
              <div v-for="(field, index) in outputFields" :key="index" class="workflow-output-row">
                <q-input v-model="field.key" dense outlined label="key" :disable="isSystemOutputField(field)" />
                <q-input v-model="field.description" dense outlined label="说明" :disable="isSystemOutputField(field)" />
                <q-select
                  v-model="field.valueType"
                  dense
                  outlined
                  label="值类型"
                  :options="valueTypeOptions"
                  :disable="isSystemOutputField(field)"
                />
                <q-btn
                  flat
                  round
                  dense
                  color="negative"
                  icon="close"
                  aria-label="删除输出字段"
                  :disable="isSystemOutputField(field)"
                  @click="deleteOutputField(index)"
                >
                  <q-tooltip>删除输出字段</q-tooltip>
                </q-btn>
              </div>
            </div>
            <div v-else class="text-caption text-muted">未声明输出字段</div>
          </div>
          <q-input v-model.number="retry" dense outlined type="number" label="失败重试次数" min="0" />
          <q-toggle v-model="requiresApproval" label="运行前人工审批" />
          <q-toggle
            v-if="nodeType !== 'approval' && nodeType !== 'close'"
            v-model="requiresForwardApproval"
            label="运行后前进审核"
          />
          <q-select
            v-if="nodeType === 'merge'"
            v-model="mergeStrategy"
            dense
            outlined
            label="合并策略"
            :options="mergeStrategyOptions"
          />
          <div class="row q-gutter-sm">
            <q-btn
              flat
              round
              color="negative"
              icon="delete"
              aria-label="删除节点"
              :disable="!selectedNode"
              @click="deleteSelectedNode"
            >
              <q-tooltip>删除节点</q-tooltip>
            </q-btn>
          </div>
          <q-separator />
          <div class="workflow-edge-list">
            <div class="text-subtitle2 text-weight-bold">连线</div>
            <q-list v-if="graph.edges.length > 0" dense separator bordered>
              <q-item
                v-for="(edge, index) in graph.edges"
                :key="`${edge.from}-${edge.to}-${index}`"
                clickable
                :active="selectedEdgeIndex === index"
                @click="selectEdge(index)"
              >
                <q-item-section>
                  <q-item-label>{{ edge.from }} → {{ edge.to }}</q-item-label>
                  <q-item-label caption>{{ edgeCaption(edge) }}</q-item-label>
                </q-item-section>
                <q-item-section side>
                  <q-btn flat round dense color="negative" icon="close" aria-label="删除连线" @click.stop="deleteEdge(index)">
                    <q-tooltip>删除连线</q-tooltip>
                  </q-btn>
                </q-item-section>
              </q-item>
            </q-list>
            <div v-else class="text-caption text-muted">暂无连线</div>
          </div>
          <template v-if="selectedEdge">
            <q-separator />
            <div class="workflow-edge-editor q-gutter-sm">
              <div class="text-subtitle2 text-weight-bold">连线条件</div>
              <q-input v-model.number="edgePriority" dense outlined type="number" label="优先级" />
              <q-btn-toggle
                v-model="conditionMode"
                spread
                unelevated
                toggle-color="primary"
                :options="conditionModeOptions"
              />
              <template v-if="conditionMode === 'field'">
                <q-select
                  v-model="conditionField"
                  dense
                  outlined
                  emit-value
                  map-options
                  label="判断字段"
                  :options="conditionFieldOptions"
                />
                <q-select v-model="conditionOp" dense outlined label="判断类型" :options="conditionOpOptions" />
                <q-input v-model="conditionValue" dense outlined label="判断值" />
              </template>
              <q-input
                v-else-if="conditionMode === 'expr'"
                v-model="conditionExpr"
                autogrow
                outlined
                type="textarea"
                label="expr 表达式"
              />
            </div>
          </template>
        </q-card-section>
        <q-card-section v-else>
          <q-banner rounded class="empty-lane-banner">拖入左侧节点开始配置流程</q-banner>
        </q-card-section>
      </q-card>
    </div>

    <q-inner-loading :showing="loading">
      <q-spinner color="primary" size="32px" />
    </q-inner-loading>
  </q-page>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue';
import { useQuasar } from 'quasar';
import { useRoute, useRouter } from 'vue-router';
import { Handle, Position, useVueFlow, VueFlow, type Connection, type EdgeMouseEvent, type NodeMouseEvent } from '@vue-flow/core';
import { Background } from '@vue-flow/background';
import { Controls } from '@vue-flow/controls';
import '@vue-flow/core/dist/style.css';
import '@vue-flow/core/dist/theme-default.css';
import '@vue-flow/controls/dist/style.css';

import { useProjects } from '@/composables/useProjects';
import {
  getWorkflowDefinition,
  saveWorkflowDefinition as saveWorkflow,
  setDefaultWorkflow,
  type WorkflowEdge,
  type WorkflowGraph,
  type WorkflowNode,
  type WorkflowOutputField,
} from '@/services/workflows';
import { completeOutputFields, systemOutputFields, workflowValueTypeOptions } from '@/services/workflowOutputFields.js';
import {
  applyWorkflowEdgeForm,
  buildFlowEdge,
  buildFlowNode,
  clientPointToFlowPoint,
  syncWorkflowNodePositions,
  workflowEdgeId,
  workflowFlowInteractionProps,
} from '@/services/workflowFlowModel.js';

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
const outputFields = ref<WorkflowOutputField[]>([]);
const retry = ref(0);
const requiresApproval = ref(false);
const requiresForwardApproval = ref(false);
const mergeStrategy = ref('merge');
const selectedEdgeIndex = ref<number | null>(null);
const edgePriority = ref(0);
const conditionMode = ref<'always' | 'field' | 'expr'>('always');
const conditionField = ref('results.outcome');
const conditionOp = ref('eq');
const conditionValue = ref('');
const conditionExpr = ref('results.outcome == "success"');
const flowNodes = ref<ReturnType<typeof buildFlowNode>[]>([]);
const flowEdges = ref<ReturnType<typeof buildFlowEdge>[]>([]);
const graph = reactive<WorkflowGraph>(defaultGraph());
const flowInteraction = workflowFlowInteractionProps();
const { fitView: fitFlowView, project: projectFlowPoint } = useVueFlow('workflow-config-flow');

const nodeTypeOptions = ['codex', 'expr', 'approval', 'merge', 'close'];
const nodePalette = [
  { value: 'codex', label: 'Codex', caption: '运行 Codex 节点' },
  { value: 'expr', label: 'Expr', caption: '用 params 计算 results' },
  { value: 'approval', label: '人工审批', caption: '等待人工确认' },
  { value: 'merge', label: '合并', caption: '合并或 rebase 到基础分支' },
  { value: 'close', label: '关闭', caption: '结束流程并关闭卡片' },
];
const mergeStrategyOptions = ['merge', 'rebase'];
const valueTypeOptions = workflowValueTypeOptions;
const conditionModeOptions = [
  { label: '总是', value: 'always' },
  { label: '字段', value: 'field' },
  { label: 'expr', value: 'expr' },
];
const conditionOpOptions = ['eq', 'ne', 'contains', 'exists', 'gt', 'gte', 'lt', 'lte'];

const projectId = computed(() => String(route.params.projectId ?? ''));
const project = computed(() => projects.value.find((item) => item.id === projectId.value));
const selectedEdge = computed(() => {
  if (selectedEdgeIndex.value == null) return null;
  return graph.edges[selectedEdgeIndex.value] ?? null;
});
const selectedNode = computed(() => currentNode());
const conditionFieldOptions = computed(() => {
  const edge = selectedEdge.value;
  const source = edge ? graph.nodes.find((node) => node.id === edge.from) : null;
  const fields = (source?.outputFields ?? [])
    .filter((field) => field.key.trim())
    .map((field) => ({
      label: `results.data.${field.key} · ${field.description || field.valueType || 'output'}`,
      value: `results.data.${field.key}`,
    }));
  const approval = source && (source.type === 'approval' || source.approval.beforeRun || source.approval.afterRun)
    ? [{ label: 'approval.approved · 人工审批结果', value: 'approval.approved' }]
    : [];
  return [
    { label: 'results.outcome', value: 'results.outcome' },
    { label: 'last.status', value: 'last.status' },
    ...fields,
    ...approval,
  ];
});
const systemOutputFieldKeys = computed(() => {
  return new Set(systemOutputFields(nodeType.value, nodeType.value === 'merge').map((field) => field.key));
});

watch(nodeType, () => {
  outputFields.value = completeOutputFields(outputFields.value, systemOutputFields(nodeType.value, nodeType.value === 'merge'));
});

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
    if (flowNodes.value.length === 0) setGraph(defaultGraph());
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
  selectedEdgeIndex.value = graph.edges.length > 0 ? 0 : null;
  refreshFlowElements();
  void fitFlowToGraph();
  loadSelectedNode();
  loadSelectedEdge();
}

async function fitFlowToGraph() {
  await nextTick();
  if (flowNodes.value.length === 0) return;
  await fitFlowView({ padding: 0.2, duration: 0 });
}

function selectNode(id: string) {
  applyCurrentEdits();
  selectedNodeId.value = id;
  loadSelectedNode();
}

function loadSelectedNode() {
  const node = currentNode();
  if (!node) {
    nodeId.value = '';
    nodeType.value = 'codex';
    nodeTitle.value = '';
    nodePrompt.value = '';
    outputFields.value = [];
    retry.value = 0;
    requiresApproval.value = false;
    requiresForwardApproval.value = false;
    mergeStrategy.value = 'merge';
    return;
  }
  nodeId.value = node.id;
  nodeType.value = node.type;
  nodeTitle.value = node.title;
  nodePrompt.value = node.prompt;
  outputFields.value = node.outputFields.map((field) => ({ ...field }));
  retry.value = node.retry.maxAttempts;
  requiresApproval.value = node.approval.beforeRun;
  requiresForwardApproval.value = node.approval.afterRun;
  mergeStrategy.value = node.merge?.strategy ?? 'merge';
}

function applyNodeEdit() {
  const node = currentNode();
  if (!node) return;
  syncWorkflowPositions();
  const oldId = node.id;
  const nextId = normalizeID(nodeId.value);
  node.id = nextId;
  node.type = nodeType.value;
  node.title = nodeTitle.value.trim() || nextId;
  node.prompt = nodePrompt.value.trim();
  const approvalBeforeRun = requiresApproval.value || nodeType.value === 'approval';
  const approvalAfterRun = requiresForwardApproval.value && nodeType.value !== 'approval' && nodeType.value !== 'close';
  const merge = nodeType.value === 'merge' ? { strategy: mergeStrategy.value } : null;
  node.outputFields = completeOutputFields(outputFields.value, systemOutputFields(nodeType.value, Boolean(merge)));
  node.retry.maxAttempts = Math.max(0, Number(retry.value) || 0);
  node.approval.beforeRun = approvalBeforeRun;
  node.approval.afterRun = approvalAfterRun;
  node.merge = merge;
  outputFields.value = node.outputFields.map((field) => ({ ...field }));
  if (oldId !== nextId) {
    graph.edges.forEach((edge) => {
      if (edge.from === oldId) edge.from = nextId;
      if (edge.to === oldId) edge.to = nextId;
    });
    selectedNodeId.value = nextId;
  }
  refreshFlowElements();
}

function dragNodeType(event: DragEvent, type: string) {
  event.dataTransfer?.setData('application/x-anycode-node-type', type);
  event.dataTransfer?.setData('text/plain', type);
}

function dropNodeType(event: DragEvent) {
  event.preventDefault();
  const type = event.dataTransfer?.getData('application/x-anycode-node-type') || event.dataTransfer?.getData('text/plain') || 'codex';
  const bounds = (event.currentTarget as HTMLElement).getBoundingClientRect();
  const point = clientPointToFlowPoint(event, bounds, projectFlowPoint);
  createNodeAt(type, point.x, point.y);
}

function createNodeAt(type: string, x: number, y: number) {
  applyNodeEdit();
  const safeType = nodeTypeOptions.includes(type) ? type : 'codex';
  const id = uniqueNodeID(safeType === 'codex' ? 'node' : safeType);
  graph.nodes.push(
    normalizeNode({
      id,
      type: safeType,
      title: defaultNodeTitle(safeType),
      prompt: defaultNodePrompt(safeType),
      position: { x, y },
      outputFields: defaultOutputFields(safeType),
      retry: { maxAttempts: 0 },
    }),
  );
  selectedNodeId.value = id;
  refreshFlowElements();
  loadSelectedNode();
}

function deleteSelectedNode() {
  const id = selectedNodeId.value;
  const index = graph.nodes.findIndex((node) => node.id === id);
  if (index < 0) return;
  graph.nodes.splice(index, 1);
  graph.edges.splice(
    0,
    graph.edges.length,
    ...graph.edges.filter((edge) => edge.from !== id && edge.to !== id),
  );
  selectedNodeId.value = graph.nodes[Math.max(0, index - 1)]?.id ?? '';
  selectedEdgeIndex.value = graph.edges.length > 0 ? Math.min(selectedEdgeIndex.value ?? 0, graph.edges.length - 1) : null;
  refreshFlowElements();
  loadSelectedNode();
  loadSelectedEdge();
}

async function saveDefinition() {
  applyCurrentEdits();
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
    $q.notify({ type: 'positive', message: '流程已保存' });
  } catch (err) {
    notifyError(err, '保存流程配置失败');
  } finally {
    saving.value = false;
  }
}

async function copyWorkflowConfig() {
  applyCurrentEdits();
  const payload = {
    name: workflowName.value.trim() || '默认流程',
    graph: { nodes: [...graph.nodes], edges: [...graph.edges] },
  };
  try {
    await navigator.clipboard.writeText(JSON.stringify(payload, null, 2));
    $q.notify({ type: 'positive', message: '流程配置已复制' });
  } catch (err) {
    notifyError(err, '复制流程配置失败');
  }
}

async function importWorkflowConfig() {
  try {
    const raw = await navigator.clipboard.readText();
    const parsed = JSON.parse(raw) as unknown;
    const graphPayload = workflowGraphFromClipboard(parsed);
    if (!graphPayload) {
      throw new Error('剪贴板内容不是流程配置');
    }
    if (parsed && typeof parsed === 'object' && 'name' in parsed && typeof parsed.name === 'string') {
      workflowName.value = parsed.name;
    }
    definitionId.value = '';
    version.value = 1;
    setGraph(graphPayload);
    $q.notify({ type: 'positive', message: '流程配置已导入，保存后生效' });
  } catch (err) {
    notifyError(err, '导入流程配置失败');
  }
}

function applyCurrentEdits() {
  syncWorkflowPositions();
  applyNodeEdit();
  applyEdgeEdit();
}

function handleFlowConnect(connection: Connection) {
  applyCurrentEdits();
  if (!connection.source || !connection.target || connection.source === connection.target) return;
  const exists = graph.edges.some((edge) => edge.from === connection.source && edge.to === connection.target);
  if (!exists) {
    graph.edges.push(
      normalizeEdge({
        from: connection.source,
        to: connection.target,
        priority: graph.edges.filter((edge) => edge.from === connection.source).length,
      }),
    );
    selectedEdgeIndex.value = graph.edges.length - 1;
    refreshFlowElements();
    loadSelectedEdge();
  }
}

function deleteEdge(index: number) {
  graph.edges.splice(index, 1);
  refreshFlowElements();
  if (selectedEdgeIndex.value === index) {
    selectedEdgeIndex.value = graph.edges.length > 0 ? Math.min(index, graph.edges.length - 1) : null;
    loadSelectedEdge();
  } else if (selectedEdgeIndex.value != null && selectedEdgeIndex.value > index) {
    selectedEdgeIndex.value -= 1;
  }
}

function selectEdge(index: number) {
  applyCurrentEdits();
  selectedEdgeIndex.value = index;
  loadSelectedEdge();
}

function handleFlowNodeClick({ node }: NodeMouseEvent) {
  selectNode(node.id);
}

function handleFlowEdgeClick({ edge }: EdgeMouseEvent) {
  const index = graph.edges.findIndex((item) => workflowEdgeId(item) === edge.id);
  if (index >= 0) selectEdge(index);
}

function handleFlowNodeDragStop() {
  syncWorkflowPositions();
  refreshFlowElements();
}

function loadSelectedEdge() {
  const edge = selectedEdge.value;
  if (!edge) return;
  edgePriority.value = edge.priority;
  conditionMode.value = edge.condition.mode === 'expr' ? 'expr' : edge.condition.field || edge.condition.op ? 'field' : 'always';
  conditionField.value = edge.condition.field || conditionFieldOptions.value[0]?.value || 'last.status';
  conditionOp.value = edge.condition.op || 'eq';
  conditionValue.value = conditionValueToInput(edge.condition.value);
  conditionExpr.value = edge.condition.expr || 'results.outcome == "success"';
}

function applyEdgeEdit() {
  const edge = selectedEdge.value;
  if (!edge) return;
  applyWorkflowEdgeForm(edge, {
    priority: edgePriority.value,
    mode: conditionMode.value,
    field: conditionField.value,
    op: conditionOp.value,
    value: conditionInputToValue(conditionValue.value),
    expr: conditionExpr.value,
  });
  refreshFlowElements();
}

function addOutputField() {
  outputFields.value.push({ key: '', description: '', valueType: 'string' });
}

function deleteOutputField(index: number) {
  if (isSystemOutputField(outputFields.value[index])) return;
  outputFields.value.splice(index, 1);
}

function currentNode() {
  return graph.nodes.find((node) => node.id === selectedNodeId.value);
}

function refreshFlowElements() {
  flowNodes.value = graph.nodes.map(buildFlowNode);
  flowEdges.value = graph.edges.map(buildFlowEdge);
}

function syncWorkflowPositions() {
  syncWorkflowNodePositions(graph.nodes, flowNodes.value);
}

function edgeCaption(edge: WorkflowEdge) {
  if (edge.condition.mode === 'expr') {
    return `priority ${edge.priority} · expr`;
  }
  if (!edge.condition.field && !edge.condition.op) {
    return `priority ${edge.priority} · always`;
  }
  return `priority ${edge.priority} · ${edge.condition.field} ${edge.condition.op}`;
}

function nodeIcon(type: string) {
  if (type === 'approval') return 'approval';
  if (type === 'merge') return 'merge_type';
  if (type === 'expr') return 'functions';
  if (type === 'close') return 'cancel';
  return 'terminal';
}

function defaultNodeTitle(type: string) {
  if (type === 'approval') return '人工审批';
  if (type === 'merge') return '合并';
  if (type === 'expr') return '表达式';
  if (type === 'close') return '关闭';
  return '新节点';
}

function defaultNodePrompt(type: string) {
  if (type === 'expr') return '{ status: params.status }';
  return '';
}

function defaultOutputFields(type: string): WorkflowOutputField[] {
  if (type === 'close') return [];
  const fields = systemOutputFields(type, type === 'merge');
  if (fields.length > 0) return fields;
  return [{ key: 'status', description: '节点执行结果，例如 passed 或 failed', valueType: 'string' }];
}

function defaultGraph(): WorkflowGraph {
  return {
    nodes: [],
    edges: [],
  };
}

function normalizeNode(node: Partial<WorkflowNode> & { id: string }): WorkflowNode {
  const type = node.type || 'codex';
  const approvalBeforeRun = Boolean(node.approval?.beforeRun) || type === 'approval';
  const merge = node.merge ? { strategy: node.merge.strategy === 'rebase' ? 'rebase' : 'merge' } : null;
  return {
    id: normalizeID(node.id),
    type,
    title: node.title || node.id,
    prompt: node.prompt || '',
    position: normalizePosition(node.position),
    outputFields: completeOutputFields(node.outputFields ?? [], systemOutputFields(type, type === 'merge' || Boolean(merge))),
    approval: {
      beforeRun: approvalBeforeRun,
      afterRun: Boolean(node.approval?.afterRun),
    },
    retry: { maxAttempts: Math.max(0, Number(node.retry?.maxAttempts ?? 0)) },
    merge,
  };
}

function normalizePosition(position: Partial<WorkflowNode['position']> | undefined) {
  return {
    x: Number(position?.x ?? 0) || 0,
    y: Number(position?.y ?? 0) || 0,
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
    mode: String(condition?.mode ?? (condition?.expr ? 'expr' : 'field')),
    field: String(condition?.field ?? ''),
    op: String(condition?.op ?? ''),
    value: condition?.value,
    expr: String(condition?.expr ?? ''),
    all: condition?.all ?? [],
    any: condition?.any ?? [],
    not: condition?.not ?? null,
  };
}

function isSystemOutputField(field: WorkflowOutputField | undefined) {
  if (!field) return false;
  return systemOutputFieldKeys.value.has(field.key.trim());
}

function conditionValueToInput(value: unknown) {
  if (value && typeof value === 'object' && 'value' in value) {
    const wrapped = value.value;
    return valueToInputString(wrapped);
  }
  if (value == null) return '';
  return valueToInputString(value);
}

function valueToInputString(value: unknown) {
  if (value == null) return '';
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  return JSON.stringify(value);
}

function conditionInputToValue(value: string) {
  const trimmed = value.trim();
  if (trimmed === 'true') return true;
  if (trimmed === 'false') return false;
  if (trimmed !== '' && !Number.isNaN(Number(trimmed))) return Number(trimmed);
  return trimmed;
}

function workflowGraphFromClipboard(value: unknown): WorkflowGraph | null {
  if (isWorkflowGraph(value)) return value;
  if (value && typeof value === 'object' && 'graph' in value && isWorkflowGraph(value.graph)) {
    return value.graph;
  }
  return null;
}

function isWorkflowGraph(value: unknown): value is WorkflowGraph {
  return Boolean(
    value &&
      typeof value === 'object' &&
      'nodes' in value &&
      'edges' in value &&
      Array.isArray(value.nodes) &&
      Array.isArray(value.edges),
  );
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
