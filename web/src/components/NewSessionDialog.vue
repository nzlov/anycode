<template>
  <q-dialog
    :model-value="dialogVisible"
    :maximized="!panel && $q.screen.lt.sm"
    :seamless="panel"
    :no-focus="panel"
    :no-refocus="panel"
    :class="{ 'new-session-panel-host': panel }"
    persistent
    @update:model-value="emitModel"
  >
    <q-card
      class="new-session-dialog app-content-dialog"
      :class="{ 'new-session-dialog--panel': panel }"
      :inert="branchesLoading"
      :aria-busy="branchesLoading || runConfigLoading"
    >
      <q-card-section v-if="!panel" class="new-session-dialog__header row items-center q-pb-sm">
        <div class="text-subtitle1 text-weight-bold">新建卡片</div>
        <q-space />
        <q-btn
          v-close-popup
          flat
          round
          dense
          class="app-icon-btn"
          icon="close"
          aria-label="关闭"
          :disable="creating"
        >
          <q-tooltip>关闭</q-tooltip>
        </q-btn>
      </q-card-section>

      <q-separator v-if="!panel" />

      <q-card-section class="new-session-body">
        <div class="new-session-grid new-session-context">
          <q-select
            v-model="projectId"
            :outlined="!panel"
            :borderless="panel"
            dense
            label="项目"
            emit-value
            map-options
            dropdown-icon=""
            :disable="creating"
            :options="projectOptions"
          />
          <div v-if="selectedProject?.isGit" class="branch-picker">
            <q-select
              v-model="branch"
              :outlined="!panel"
              :borderless="panel"
              dense
              label="基础分支"
              class="branch-picker__select"
              dropdown-icon=""
              :disable="creating || branchesLoading"
              :loading="branchesLoading"
              :options="branchOptions"
            />
            <q-btn
              flat
              round
              dense
              class="app-icon-btn"
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
            :outlined="!panel"
            :borderless="panel"
            dense
            label="优先级"
            class="new-session-priority"
            emit-value
            map-options
            dropdown-icon=""
            :disable="creating"
            :options="priorityOptions"
          />
        </div>

        <q-banner v-if="runConfigError" dense rounded class="new-session-config-error">
          <template #avatar>
            <q-icon name="error_outline" color="negative" />
          </template>
          {{ runConfigError }}
          <template #action>
            <q-btn
              flat
              round
              dense
              class="app-icon-btn"
              icon="refresh"
              aria-label="重试加载项目运行参数"
              @click="retryLastConfig"
            >
              <q-tooltip>重试</q-tooltip>
            </q-btn>
          </template>
        </q-banner>

        <CodexPromptComposer
          v-model:prompt="prompt"
          v-model:files="files"
          v-model:model="model"
          v-model:effort="effort"
          v-model:permission="permission"
          v-model:fast="fast"
          :title="panel ? '' : '提示词'"
          :compact="panel"
          :show-badge="!panel"
          :disabled="creating || runConfigLoading || Boolean(runConfigError)"
          @submit="createSession(preferredAvailableMode)"
        >
          <template #actions>
            <q-btn
              v-if="canUseWorkflowMode"
              unelevated
              class="app-command-btn new-session-launch-btn"
              :class="{
                'new-session-launch-btn--preferred': preferredAvailableMode === 'workflow',
              }"
              :color="preferredAvailableMode === 'workflow' ? 'positive' : undefined"
              :text-color="preferredAvailableMode === 'workflow' ? 'dark' : undefined"
              icon="account_tree"
              label="流程模式"
              no-caps
              :disable="creating || !branchSelectionReady"
              :loading="launchingMode === 'workflow'"
              @click="createSession('workflow')"
            />
            <q-btn
              unelevated
              class="app-command-btn new-session-launch-btn"
              :class="{ 'new-session-launch-btn--preferred': preferredAvailableMode === 'chat' }"
              :color="preferredAvailableMode === 'chat' ? 'positive' : undefined"
              :text-color="preferredAvailableMode === 'chat' ? 'dark' : undefined"
              icon="forum"
              label="会话模式"
              no-caps
              :disable="creating || !branchSelectionReady"
              :loading="launchingMode === 'chat'"
              @click="createSession('chat')"
            />
          </template>
        </CodexPromptComposer>
      </q-card-section>

      <q-inner-loading :showing="branchesLoading" color="primary" />
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { Notify, useQuasar } from 'quasar';

import CodexPromptComposer from '@/components/CodexPromptComposer.vue';
import { normalizePermissionMode } from '@/components/promptOptions';
import { useProjectBranches } from '@/composables/useProjectBranches';
import { useProjects } from '@/composables/useProjects';
import { deleteStagedAttachment, stageAttachment } from '@/services/attachments';
import { graphqlFetch } from '@/services/graphqlClient';
import {
  getLastSessionConfig,
  type CreateSessionInput,
  type SessionPriority,
} from '@/services/sessions';
import { getWorkflowDefinition } from '@/services/workflows';

const props = defineProps<{
  modelValue: boolean;
  defaultProjectId?: string;
  panel?: boolean;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  create: [];
}>();

const $q = useQuasar();
const { projects, projectOptions, loadProjects } = useProjects();
const { branchCache, branchLoading, loadProjectBranches } = useProjectBranches();
const lastProjectStorageKey = 'anycode.lastNewSessionProjectId';
const lastLaunchModeStorageKey = 'anycode.lastNewSessionLaunchMode';
const projectId = ref(projects.value[0]?.id ?? '');
const branch = ref('');
const preferredMode = ref<'workflow' | 'chat'>(storedLaunchMode());
const priority = ref<SessionPriority>('medium');
const prompt = ref('');
const files = ref<File[]>([]);
const model = ref('');
const effort = ref('');
const permission = ref(normalizePermissionMode('workspace-write'));
const fast = ref(false);
const creating = ref(false);
const launchingMode = ref<'workflow' | 'chat' | ''>('');
const runConfigLoading = ref(false);
const runConfigError = ref('');
const workflowAvailable = ref(false);
const workflowAvailabilityToken = ref(0);
const lastConfigRequestToken = ref(0);
const dialogVisible = computed(() => Boolean(props.panel || props.modelValue));

const branchOptions = computed(() => {
  return projectBranchState(projectId.value).branches;
});
const selectedProject = computed(() =>
  projects.value.find((project) => project.id === projectId.value),
);
const branchesLoading = computed(() => Boolean(branchLoading.value[projectId.value]));
const branchSelectionReady = computed(() => {
  if (runConfigLoading.value || runConfigError.value) return false;
  if (!selectedProject.value) return false;
  if (!selectedProject.value.isGit) return true;
  const state = branchCache.value[projectId.value];
  return !branchesLoading.value && Boolean(state?.branches.includes(branch.value));
});
const canUseWorkflowMode = computed(() => workflowAvailable.value);
const preferredAvailableMode = computed<'workflow' | 'chat'>(() =>
  preferredMode.value === 'workflow' && canUseWorkflowMode.value ? 'workflow' : 'chat',
);

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

function storedLaunchMode(): 'workflow' | 'chat' {
  try {
    return window.localStorage.getItem(lastLaunchModeStorageKey) === 'workflow'
      ? 'workflow'
      : 'chat';
  } catch {
    return 'chat';
  }
}

function rememberLaunchMode(value: 'workflow' | 'chat') {
  preferredMode.value = value;
  try {
    window.localStorage.setItem(lastLaunchModeStorageKey, value);
  } catch {
    // Ignore storage failures; the current page still remembers the launch mode.
  }
}

function resetRunConfig() {
  model.value = '';
  effort.value = '';
  permission.value = normalizePermissionMode('workspace-write');
  fast.value = false;
}

async function loadLastConfigForProject(value: string) {
  const token = lastConfigRequestToken.value + 1;
  lastConfigRequestToken.value = token;
  runConfigError.value = '';
  resetRunConfig();
  if (!value) {
    runConfigLoading.value = false;
    return;
  }
  runConfigLoading.value = true;
  try {
    const config = await getLastSessionConfig(value);
    if (lastConfigRequestToken.value !== token || projectId.value !== value) return;
    if (!config) return;
    model.value = config.codexModel;
    effort.value = config.reasoningEffort;
    permission.value = normalizePermissionMode(config.permissionMode);
    fast.value = config.fastMode;
  } catch (error) {
    if (lastConfigRequestToken.value !== token || projectId.value !== value) return;
    runConfigError.value = `获取项目运行参数失败：${errorMessage(error)}`;
  } finally {
    if (lastConfigRequestToken.value === token) {
      runConfigLoading.value = false;
    }
  }
}

function retryLastConfig() {
  if (projectId.value) void loadLastConfigForProject(projectId.value);
}

function selectInitialProject() {
  const fallback = projects.value[0]?.id ?? '';
  const candidates = [props.defaultProjectId, storedProjectId(), projectId.value, fallback].filter(
    Boolean,
  );
  const nextProjectId =
    candidates.find((candidate) => projects.value.some((project) => project.id === candidate)) ??
    fallback;
  if (!nextProjectId) {
    if (!projectId.value) return false;
    projectId.value = '';
    branch.value = '';
    return true;
  }
  if (projectId.value === nextProjectId) {
    branch.value = '';
    void loadBranchesForProject(nextProjectId, { refresh: true });
    return false;
  }
  projectId.value = nextProjectId;
  return true;
}

async function loadWorkflowAvailability() {
  const token = workflowAvailabilityToken.value + 1;
  workflowAvailabilityToken.value = token;
  const workflowId = selectedProject.value?.defaultWorkflowId ?? '';
  if (!workflowId) {
    workflowAvailable.value = false;
    return;
  }
  try {
    const definition = await getWorkflowDefinition(workflowId);
    if (workflowAvailabilityToken.value !== token) return;
    workflowAvailable.value = Boolean(definition?.graph.nodes.length);
  } catch {
    if (workflowAvailabilityToken.value === token) {
      workflowAvailable.value = false;
    }
  }
}

function projectBranchState(value: string) {
  return branchCache.value[value] ?? { defaultBranch: '', branches: [] };
}

async function loadBranchesForProject(value: string, options: { refresh?: boolean } = {}) {
  const project = projects.value.find((item) => item.id === value);
  if (!project?.isGit) {
    if (projectId.value === value) branch.value = '';
    return;
  }
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

async function createSession(requestedMode: 'workflow' | 'chat') {
  if (creating.value) return;
  if (!branchSelectionReady.value) {
    Notify.create({
      type: 'negative',
      icon: 'error',
      position: 'top-right',
      message: '请等待当前项目分支加载完成',
      timeout: 5000,
      actions: [{ icon: 'close', color: 'white', round: true }],
    });
    return;
  }

  const config: CreateSessionInput['config'] = {
    codexModel: model.value,
    reasoningEffort: effort.value,
    permissionMode: permission.value,
    fastMode: fast.value,
  };
  const input: CreateSessionInput = {
    projectId: projectId.value,
    requirement: prompt.value,
    mode: requestedMode === 'workflow' && canUseWorkflowMode.value ? 'workflow' : 'chat',
    priority: priority.value,
    config,
  };
  if (selectedProject.value?.isGit) {
    input.baseBranch = branch.value;
  }

  const selectedFiles = [...files.value];
  const stagedAttachmentIds: string[] = [];
  let phase: 'upload' | 'create' = selectedFiles.length > 0 ? 'upload' : 'create';

  creating.value = true;
  launchingMode.value = requestedMode;
  try {
    for (const file of selectedFiles) {
      const attachment = await stageAttachment(file);
      stagedAttachmentIds.push(attachment.id);
    }

    phase = 'create';
    if (stagedAttachmentIds.length > 0) {
      input.stagedAttachmentIds = stagedAttachmentIds;
    }
    await createSessionRequest(input);
    rememberProjectId(input.projectId);
    rememberLaunchMode(input.mode);
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
    launchingMode.value = '';
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

watch(dialogVisible, (open) => {
  if (!open) {
    lastConfigRequestToken.value += 1;
    runConfigLoading.value = false;
    runConfigError.value = '';
    return;
  }
  const projectChanged = selectInitialProject();
  if (!projectChanged && projectId.value) void loadLastConfigForProject(projectId.value);
  void loadProjects().then(() => {
    selectInitialProject();
    void loadWorkflowAvailability();
  });
});

watch(projectId, (value, previous) => {
  if (!value || value === previous) return;
  branch.value = '';
  void loadBranchesForProject(value, { refresh: true });
  void loadWorkflowAvailability();
  if (dialogVisible.value) void loadLastConfigForProject(value);
});

watch(
  () => props.defaultProjectId,
  (value, previous) => {
    if (!dialogVisible.value || value === previous) return;
    const projectChanged = selectInitialProject();
    if (!projectChanged && projectId.value) void loadLastConfigForProject(projectId.value);
  },
);

onMounted(() => {
  if (dialogVisible.value && projectId.value) void loadLastConfigForProject(projectId.value);
  void loadProjects().then(loadWorkflowAvailability);
});
</script>
