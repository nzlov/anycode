<template>
  <q-dialog :model-value="modelValue" persistent @update:model-value="emitModel">
    <q-card class="new-session-dialog">
      <q-card-section class="row items-center q-pb-sm">
        <div>
          <div class="text-subtitle1 text-weight-bold">新建卡片</div>
          <div class="text-caption text-muted">配置项目、分支、模式和 Codex 运行参数</div>
        </div>
        <q-space />
        <q-btn v-close-popup flat round dense icon="close" aria-label="关闭" />
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
            :options="projectOptions"
          />
          <q-select v-model="branch" outlined dense label="基础分支" :options="branchOptions" />
          <q-btn-toggle
            v-model="mode"
            spread
            no-caps
            toggle-color="primary"
            :options="modeOptions"
          />
        </div>

        <div class="prompt-shell">
          <div class="attachment-zone">
            <div v-if="files.length > 0" class="attachment-list">
              <q-chip
                v-for="file in files"
                :key="`${file.name}-${file.size}`"
                removable
                square
                :clickable="canPreview(file)"
                class="attachment-chip"
                :icon="fileIcon(file)"
                @click="openPreview(file)"
                @remove="removeFile(file)"
              >
                <span class="ellipsis">{{ file.name }}</span>
                <q-icon v-if="canPreview(file)" name="visibility" class="q-ml-sm" />
              </q-chip>
            </div>
            <q-file
              v-model="files"
              outlined
              dense
              multiple
              append
              label="添加附件"
              class="file-picker"
            >
              <template #prepend>
                <q-icon name="attach_file" />
              </template>
            </q-file>
          </div>

          <q-input
            v-model.trim="prompt"
            autogrow
            borderless
            type="textarea"
            class="prompt-input"
            placeholder="描述你希望 Codex 完成的任务"
          />
        </div>
      </q-card-section>

      <q-separator />

      <q-card-actions class="new-session-actions">
        <q-btn flat round icon="admin_panel_settings" color="primary" aria-label="运行权限">
          <q-tooltip>运行权限：workspace-write</q-tooltip>
        </q-btn>
        <q-select
          v-model="model"
          dense
          borderless
          emit-value
          map-options
          class="compact-select"
          :options="modelOptions"
        />
        <q-select
          v-model="effort"
          dense
          borderless
          emit-value
          map-options
          class="compact-select"
          :options="effortOptions"
        />
        <q-space />
        <q-btn
          unelevated
          color="primary"
          icon="add"
          label="创建卡片"
          no-caps
          @click="createSession"
        />
      </q-card-actions>
    </q-card>
  </q-dialog>

  <q-dialog v-model="previewOpen">
    <q-card class="attachment-preview-card">
      <q-card-section class="row items-center q-pb-sm">
        <div class="text-subtitle2 text-weight-bold ellipsis">{{ previewName }}</div>
        <q-space />
        <q-btn flat round dense icon="close" aria-label="关闭预览" @click="closePreview" />
      </q-card-section>
      <q-separator />
      <q-card-section class="attachment-preview-body">
        <img
          v-if="previewKind === 'image'"
          :src="previewUrl"
          alt=""
          class="attachment-preview-media"
        />
        <video
          v-else-if="previewKind === 'video'"
          :src="previewUrl"
          class="attachment-preview-media"
          controls
        />
      </q-card-section>
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';

import { useProjects } from '@/composables/useProjects';

defineProps<{
  modelValue: boolean;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  create: [];
}>();

const { projects, projectOptions, loadProjects } = useProjects();
const projectId = ref(projects.value[0]?.id ?? '');
const branch = ref(projects.value[0]?.defaultBranch ?? 'main');
const mode = ref<'workflow' | 'chat'>('workflow');
const prompt = ref('');
const files = ref<File[]>([]);
const model = ref('cli-default');
const effort = ref('medium');
const previewOpen = ref(false);
const previewName = ref('');
const previewKind = ref<'image' | 'video' | ''>('');
const previewUrl = ref('');

const branchOptions = computed(() => {
  const selectedProject = projects.value.find((project) => project.id === projectId.value);
  const defaultBranch = selectedProject?.defaultBranch ?? 'main';
  return Array.from(new Set([defaultBranch, 'main', 'master', 'dev-foundation']));
});

const modeOptions = [
  { label: '流程模式', value: 'workflow', icon: 'account_tree' },
  { label: '会话模式', value: 'chat', icon: 'forum' },
];

const modelOptions = [
  { label: 'Codex 默认模型', value: 'cli-default' },
  { label: 'gpt-5.4', value: 'gpt-5.4' },
  { label: 'gpt-5.4-mini', value: 'gpt-5.4-mini' },
];

const effortOptions = [
  { label: '中等思考', value: 'medium' },
  { label: '低思考', value: 'low' },
  { label: '高思考', value: 'high' },
];

function emitModel(value: boolean) {
  emit('update:modelValue', value);
}

function fileIcon(file: File) {
  if (file.type.startsWith('image/')) return 'image';
  if (file.type.startsWith('video/')) return 'movie';
  return 'description';
}

function canPreview(file: File) {
  return file.type.startsWith('image/') || file.type.startsWith('video/');
}

function openPreview(file: File) {
  if (!canPreview(file)) return;
  revokePreviewUrl();
  previewName.value = file.name;
  previewKind.value = file.type.startsWith('image/') ? 'image' : 'video';
  previewUrl.value = URL.createObjectURL(file);
  previewOpen.value = true;
}

function closePreview() {
  previewOpen.value = false;
  previewName.value = '';
  previewKind.value = '';
  revokePreviewUrl();
}

function revokePreviewUrl() {
  if (previewUrl.value) {
    URL.revokeObjectURL(previewUrl.value);
    previewUrl.value = '';
  }
}

function removeFile(file: File) {
  files.value = files.value.filter((item) => item !== file);
}

function createSession() {
  emit('create');
  emit('update:modelValue', false);
}

watch(
  projects,
  (items) => {
    const currentProject = items.find((project) => project.id === projectId.value);
    if (!currentProject && items[0]) {
      projectId.value = items[0].id;
      branch.value = items[0].defaultBranch;
      return;
    }
    if (currentProject) {
      branch.value = currentProject.defaultBranch;
    }
  },
  { immediate: true },
);

onMounted(() => {
  void loadProjects();
});

onBeforeUnmount(revokePreviewUrl);
</script>
