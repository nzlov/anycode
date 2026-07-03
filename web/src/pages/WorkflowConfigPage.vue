<template>
  <q-page class="page-shell">
    <div class="page-heading">
      <div>
        <div class="text-h5 text-weight-bold">流程配置</div>
        <div class="text-body2 text-muted">
          {{ projectName }} · 节点、重试、审批、条件分支保存为业务 JSON
        </div>
      </div>
      <div class="row items-center q-gutter-sm">
        <q-chip v-if="definitionId" dense outline color="primary">v{{ version }}</q-chip>
        <q-btn
          unelevated
          color="primary"
          icon="save"
          label="保存为默认流程"
          no-caps
          :loading="saving"
          @click="saveDefinition"
        />
      </div>
    </div>

    <q-banner v-if="error" rounded class="q-mb-md bg-red-1 text-negative">
      {{ error }}
    </q-banner>

    <div class="workflow-layout">
      <q-card flat bordered class="workflow-list">
        <q-card-section class="row items-center">
          <div class="text-subtitle1 text-weight-bold">节点</div>
          <q-space />
          <q-btn
            flat
            round
            dense
            icon="add"
            color="primary"
            aria-label="新增节点"
            @click="addNode"
          />
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
              <q-item-label caption>{{ node.type }} · retry {{ node.retry.maxAttempts }}</q-item-label>
            </q-item-section>
          </q-item>
        </q-list>
      </q-card>

      <q-card flat bordered class="workflow-canvas">
        <q-card-section class="q-gutter-md">
          <q-input v-model="workflowName" dense outlined label="流程名称" />
          <div class="canvas-grid">
            <button
              v-for="node in graph.nodes"
              :key="node.id"
              type="button"
              class="workflow-node"
              :class="{ 'workflow-node--active': node.id === selectedNodeId }"
              @click="selectNode(node.id)"
            >
              <q-icon :name="nodeIcon(node.type)" color="primary" />
              <div>
                <div class="text-weight-medium">{{ node.title || node.id }}</div>
                <div class="text-caption text-muted">{{ nodeCaption(node.id) }}</div>
              </div>
            </button>
          </div>
          <q-separator />
          <div class="text-caption text-muted">出边按 priority 从小到大评估，条件使用 JSON AST。</div>
        </q-card-section>
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
          <q-input
            v-model.number="retry"
            dense
            outlined
            type="number"
            label="失败重试次数"
            min="0"
          />
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
              color="negative"
              icon="delete"
              label="删除"
              no-caps
              :disable="graph.nodes.length <= 1"
              @click="deleteSelectedNode"
            />
          </div>
          <q-separator />
          <q-input
            v-model="edgesText"
            autogrow
            outlined
            type="textarea"
            label="出边 JSON"
            hint="数组格式：[{ from, to, priority, condition }]"
          />
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

const route = useRoute();
const router = useRouter();
const $q = useQuasar();
const { projects, loadProjects } = useProjects();

const loading = ref(false);
const saving = ref(false);
const error = ref('');
const definitionId = ref('');
const version = ref(1);
const workflowName = ref('默认流程');
const selectedNodeId = ref('');
const edgesText = ref('[]');
const nodeId = ref('');
const nodeType = ref('codex');
const nodeTitle = ref('');
const nodePrompt = ref('');
const retry = ref(0);
const requiresApproval = ref(false);
const mergeStrategy = ref('merge');
const graph = reactive<WorkflowGraph>(defaultGraph());

const nodeTypeOptions = ['codex', 'approval', 'merge'];
const mergeStrategyOptions = ['merge', 'rebase'];

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
    error.value = errorMessage(err);
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
  edgesText.value = JSON.stringify(graph.edges, null, 2);
  loadSelectedNode();
}

function selectNode(id: string) {
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
    selectedNodeId.value = nextId;
    edgesText.value = JSON.stringify(graph.edges, null, 2);
  }
}

function addNode() {
  applyNodeEdit();
  const id = uniqueNodeID('node');
  graph.nodes.push(normalizeNode({ id, type: 'codex', title: '新节点', prompt: '', retry: { maxAttempts: 0 } }));
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
  selectedNodeId.value = graph.nodes[Math.max(0, index - 1)]?.id ?? '';
  edgesText.value = JSON.stringify(graph.edges, null, 2);
  loadSelectedNode();
}

async function saveDefinition() {
  error.value = '';
  applyNodeEdit();
  let edges: WorkflowEdge[];
  try {
    edges = JSON.parse(edgesText.value) as WorkflowEdge[];
  } catch {
    error.value = '出边 JSON 不是合法数组';
    return;
  }
  if (!Array.isArray(edges)) {
    error.value = '出边 JSON 必须是数组';
    return;
  }
  graph.edges.splice(0, graph.edges.length, ...edges.map(normalizeEdge));
  edgesText.value = JSON.stringify(graph.edges, null, 2);

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
    error.value = errorMessage(err);
  } finally {
    saving.value = false;
  }
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

function errorMessage(err: unknown) {
  return err instanceof Error ? err.message : '流程配置操作失败';
}
</script>
