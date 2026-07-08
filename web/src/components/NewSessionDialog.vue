<template>
  <q-dialog :model-value="modelValue" persistent @update:model-value="emitModel">
    <q-card class="new-session-dialog">
      <q-card-section class="row items-center q-pb-sm">
        <div>
          <div class="text-subtitle1 text-weight-bold">新建卡片</div>
          <div class="text-caption text-muted">配置项目、分支和 Codex 运行参数</div>
        </div>
        <q-space />
        <q-btn v-close-popup flat round dense icon="close" aria-label="关闭" :disable="creating">
          <q-tooltip>关闭</q-tooltip>
        </q-btn>
      </q-card-section>

      <q-separator />

      <q-card-section class="new-session-body">
        <div class="new-session-grid">
          <q-select
            v-model="projectId"
            outlined
            dense
            label="项目"
            emit-value
            map-options
            :disable="creating"
            :options="projectOptions"
          />
          <div v-if="selectedProject?.isGit" class="branch-picker">
            <q-select
              v-model="branch"
              outlined
              dense
              label="基础分支"
              class="branch-picker__select"
              :disable="creating || branchesLoading"
              :loading="branchesLoading"
              :options="branchOptions"
            />
            <q-btn
              flat
              round
              dense
              icon="refresh"
              aria-label="刷新分支"
              :disable="creating || branchesLoading"
              :loading="branchesLoading"
              @click="refreshProjectBranches(projectId)"
            >
              <q-tooltip>刷新分支</q-tooltip>
            </q-btn>
          </div>
          <q-select
            v-model="priority"
            outlined
            dense
            label="优先级"
            emit-value
            map-options
            :disable="creating"
            :options="priorityOptions"
          />
          <q-btn-toggle
            v-if="canUseWorkflowMode"
            v-model="mode"
            spread
            no-caps
            toggle-color="dark"
            :disable="creating || workflowAvailabilityLoading"
            :options="modeOptions"
          />
        </div>

        <PromptComposer
          v-model:prompt="prompt"
          v-model:files="files"
          v-model:model="model"
          v-model:effort="effort"
          v-model:permission="permission"
          title="提示词"
          :disabled="creating"
        >
          <template #actions>
            <q-btn
              unelevated
              color="positive"
              text-color="dark"
              icon="send"
              label="创建卡片"
              no-caps
              :disable="creating"
              :loading="creating"
              @click="createSession"
            />
          </template>
        </PromptComposer>
      </q-card-section>
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { Notify } from 'quasar';

import PromptComposer from '@/components/PromptComposer.vue';
import {
  firstCodexModelValue,
  normalizeCodexModel,
  normalizePermissionMode,
  normalizeReasoningEffort,
} from '@/components/promptOptions';
import { useProjectBranches } from '@/composables/useProjectBranches';
import { useProjects } from '@/composables/useProjects';
import { deleteStagedAttachment, stageAttachment } from '@/services/attachments';
import { graphqlFetch } from '@/services/graphqlClient';
import type { CreateSessionInput, SessionPriority } from '@/services/sessions';
import { getWorkflowDefinition } from '@/services/workflows';

const props = defineProps<{
  modelValue: boolean;
  defaultProjectId?: string;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  create: [];
}>();

const { projects, projectOptions, loadProjects } = useProjects();
const { branchCache, branchLoading, loadProjectBranches } = useProjectBranches();
const projectId = ref(projects.value[0]?.id ?? '');
const branch = ref('main');
const mode = ref<'workflow' | 'chat'>('chat');
const priority = ref<SessionPriority>('medium');
const prompt = ref('');
const files = ref<File[]>([]);
const model = ref(firstCodexModelValue());
const effort = ref(normalizeReasoningEffort(model.value, ''));
const permission = ref(normalizePermissionMode('workspace-write'));
const creating = ref(false);
const workflowAvailabilityLoading = ref(false);
const workflowAvailable = ref(false);
const workflowAvailabilityToken = ref(0);
const lastProjectStorageKey = 'anycode.lastNewSessionProjectId';
const lastSessionConfigStorageKey = 'anycode.lastSessionConfig';

const branchOptions = computed(() => {
  return projectBranchState(projectId.value).branches;
});
const selectedProject = computed(() =>
  projects.value.find((project) => project.id === projectId.value),
);
const branchesLoading = computed(() => Boolean(branchLoading.value[projectId.value]));
const canUseWorkflowMode = computed(() => workflowAvailable.value);

const modeOptions = [
  { label: '流程模式', value: 'workflow', icon: 'account_tree' },
  { label: '会话模式', value: 'chat', icon: 'forum' },
];

const priorityOptions = [
  { label: '高', value: 'high', icon: 'keyboard_double_arrow_up' },
  { label: '中', value: 'medium', icon: 'drag_handle' },
  { label: '低', value: 'low', icon: 'keyboard_double_arrow_down' },
];

function emitModel(value: boolean) {
  emit('update:modelValue', value);
}

function storedProjectId() {
  try {
    return window.localStorage.getItem(lastProjectStorageKey) ?? '';
  } catch {
    return '';
  }
}

function rememberProjectId(value: string) {
  try {
    window.localStorage.setItem(lastProjectStorageKey, value);
  } catch {
    // Ignore storage failures; project selection still works for the current dialog.
  }
}

function storedSessionConfig() {
  try {
    const raw = window.localStorage.getItem(lastSessionConfigStorageKey);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as unknown;
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed)
      ? (parsed as Record<string, string>)
      : {};
  } catch {
    return {};
  }
}

function rememberSessionConfig() {
  try {
    window.localStorage.setItem(
      lastSessionConfigStorageKey,
      JSON.stringify({
        codexModel: model.value,
        reasoningEffort: effort.value,
        permissionMode: permission.value,
      }),
    );
  } catch {
    // Ignore storage failures; the current session still uses the selected config.
  }
}

function selectInitialRunConfig() {
  const stored = storedSessionConfig();
  const nextModel = normalizeCodexModel(stored.codexModel ?? model.value);
  model.value = nextModel;
  effort.value = normalizeReasoningEffort(nextModel, stored.reasoningEffort ?? effort.value);
  permission.value = normalizePermissionMode(stored.permissionMode ?? permission.value);
}

function selectInitialProject() {
  const fallback = projects.value[0]?.id ?? '';
  const candidates = [props.defaultProjectId, storedProjectId(), projectId.value, fallback].filter(
    Boolean,
  );
  const nextProjectId =
    candidates.find((candidate) => projects.value.some((project) => project.id === candidate)) ??
    fallback;
  if (!nextProjectId) return;
  projectId.value = nextProjectId;
  branch.value = projectBranch(nextProjectId);
  void loadBranchesForProject(nextProjectId);
}

async function loadWorkflowAvailability() {
  const token = workflowAvailabilityToken.value + 1;
  workflowAvailabilityToken.value = token;
  const workflowId = selectedProject.value?.defaultWorkflowId ?? '';
  if (!workflowId) {
    workflowAvailable.value = false;
    mode.value = 'chat';
    return;
  }
  workflowAvailabilityLoading.value = true;
  try {
    const definition = await getWorkflowDefinition(workflowId);
    if (workflowAvailabilityToken.value !== token) return;
    workflowAvailable.value = Boolean(definition?.graph.nodes.length);
    if (!workflowAvailable.value) {
      mode.value = 'chat';
    }
  } catch {
    if (workflowAvailabilityToken.value === token) {
      workflowAvailable.value = false;
      mode.value = 'chat';
    }
  } finally {
    if (workflowAvailabilityToken.value === token) {
      workflowAvailabilityLoading.value = false;
    }
  }
}

function projectBranch(value: string) {
  return projectBranchState(value).defaultBranch;
}

function projectBranchState(value: string) {
  return branchCache.value[value] ?? { defaultBranch: 'main', branches: ['main'] };
}

async function loadBranchesForProject(value: string, options: { refresh?: boolean } = {}) {
  const project = projects.value.find((item) => item.id === value);
  if (!project?.isGit) return;
  try {
    const state = await loadProjectBranches(value, options);
    if (projectId.value !== value) return;
    branch.value = state.branches.includes(branch.value) ? branch.value : state.defaultBranch;
  } catch (error) {
    Notify.create({
      type: 'negative',
      icon: 'error',
      position: 'top-right',
      message: `获取分支失败：${errorMessage(error)}`,
      timeout: 5000,
      actions: [{ icon: 'close', color: 'white', round: true }],
    });
  }
}

async function refreshProjectBranches(value: string) {
  await loadBranchesForProject(value, { refresh: true });
}

async function createSession() {
  const config: CreateSessionInput['config'] = {
    codexModel: model.value,
    reasoningEffort: effort.value,
    permissionMode: permission.value,
  };

  const selectedFiles = [...files.value];
  const stagedAttachmentIds: string[] = [];
  let phase: 'upload' | 'create' = selectedFiles.length > 0 ? 'upload' : 'create';

  creating.value = true;
  try {
    for (const file of selectedFiles) {
      const attachment = await stageAttachment(file);
      stagedAttachmentIds.push(attachment.id);
    }

    phase = 'create';
    const input: CreateSessionInput = {
      projectId: projectId.value,
      requirement: prompt.value,
      mode: canUseWorkflowMode.value ? mode.value : 'chat',
      priority: priority.value,
      config,
    };
    if (selectedProject.value?.isGit) {
      input.baseBranch = branch.value;
    }
    if (stagedAttachmentIds.length > 0) {
      input.stagedAttachmentIds = stagedAttachmentIds;
    }
    await createSessionRequest(input);
    rememberProjectId(projectId.value);
    rememberSessionConfig();
    files.value = [];
    prompt.value = '';
    emit('create');
    emit('update:modelValue', false);
  } catch (error) {
    const cleanupError = await cleanupStagedAttachments(stagedAttachmentIds);
    if (!wasNotified(error) || cleanupError) {
      const prefix = phase === 'upload' ? '附件上传失败' : '创建卡片失败';
      Notify.create({
        type: 'negative',
        icon: 'error',
        position: 'top-right',
        message: cleanupError
          ? `${prefix}：${errorMessage(error)}；${cleanupError}`
          : `${prefix}：${errorMessage(error)}`,
        timeout: 5000,
        actions: [{ icon: 'close', color: 'white', round: true }],
      });
    }
  } finally {
    creating.value = false;
  }
}

async function createSessionRequest(input: CreateSessionInput) {
  await graphqlFetch<{ createSession: { id: string } }, { input: CreateSessionInput }>({
    query: `
      mutation CreateSession($input: CreateSessionInput!) {
        createSession(input: $input) {
          id
        }
      }
    `,
    variables: { input },
  });
}

async function cleanupStagedAttachments(ids: string[]) {
  if (ids.length === 0) return '';
  const results = await Promise.allSettled(ids.map((id) => deleteStagedAttachment(id)));
  const failed = results.find((result) => result.status === 'rejected');
  if (!failed || failed.status !== 'rejected') return '';
  return `已上传附件清理失败：${errorMessage(failed.reason)}`;
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

function wasNotified(error: unknown) {
  return Boolean(error && typeof error === 'object' && '__anycodeNotified' in error);
}

watch(
  projects,
  () => {
    selectInitialProject();
    void loadWorkflowAvailability();
  },
  { immediate: true },
);

watch(
  () => props.modelValue,
  (open) => {
    if (!open) return;
    selectInitialRunConfig();
    void loadProjects().then(() => {
      selectInitialProject();
      void loadWorkflowAvailability();
    });
  },
);

watch(model, (value) => {
  const nextModel = normalizeCodexModel(value);
  if (nextModel !== value) {
    model.value = nextModel;
    return;
  }
  effort.value = normalizeReasoningEffort(nextModel, effort.value);
});

watch(projectId, (value, previous) => {
  if (!value || value === previous) return;
  branch.value = projectBranch(value);
  void loadBranchesForProject(value);
  void loadWorkflowAvailability();
});

onMounted(() => {
  selectInitialRunConfig();
  void loadProjects().then(loadWorkflowAvailability);
});
</script>
