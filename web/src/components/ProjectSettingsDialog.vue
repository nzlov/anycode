<template>
  <q-dialog
    :model-value="modelValue"
    :maximized="$q.screen.lt.sm"
    :persistent="saving"
    aria-label="项目设置"
    @update:model-value="emitModel"
  >
    <q-card class="project-settings-dialog">
      <q-card-section class="row items-center q-pb-sm">
        <div>
          <div class="text-subtitle1 text-weight-bold">项目设置</div>
          <div class="text-caption text-muted">{{ project?.name }}</div>
        </div>
        <q-space />
        <q-btn
          v-close-popup
          flat
          round
          dense
          class="app-icon-btn"
          icon="close"
          aria-label="关闭"
          :disable="saving"
        >
          <q-tooltip>关闭</q-tooltip>
        </q-btn>
      </q-card-section>

      <q-separator />

      <q-card-section class="project-settings-dialog__body">
        <q-input
          v-model="worktreeInitCommand"
          outlined
          type="textarea"
          label="工作树初始化命令"
          :rows="10"
        />
      </q-card-section>

      <q-separator />

      <q-card-actions align="right">
        <q-btn
          v-close-popup
          flat
          round
          class="app-icon-btn"
          icon="close"
          color="primary"
          aria-label="取消"
          :disable="saving"
        >
          <q-tooltip>取消</q-tooltip>
        </q-btn>
        <q-btn
          unelevated
          class="app-command-btn"
          color="positive"
          text-color="dark"
          icon="save"
          label="保存"
          no-caps
          :loading="saving"
          :disable="!project"
          @click="save"
        />
      </q-card-actions>
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue';
import { useQuasar } from 'quasar';

import { useProjects } from '@/composables/useProjects';
import type { ProjectSummary } from '@/services/projects';

const props = defineProps<{
  modelValue: boolean;
  project: ProjectSummary | null;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
}>();

const $q = useQuasar();
const { updateProjectSettingsById } = useProjects();
const worktreeInitCommand = ref('');
const saving = ref(false);

watch(
  () => [props.modelValue, props.project?.id] as const,
  ([open]) => {
    if (open) {
      worktreeInitCommand.value = props.project?.worktreeInitCommand ?? '';
    }
  },
);

function emitModel(value: boolean) {
  emit('update:modelValue', value);
}

async function save() {
  if (!props.project) return;
  saving.value = true;
  try {
    await updateProjectSettingsById({
      projectId: props.project.id,
      worktreeInitCommand: worktreeInitCommand.value,
    });
    $q.notify({ type: 'positive', message: '项目设置已保存' });
    emit('update:modelValue', false);
  } catch (err) {
    if (!wasNotified(err)) {
      $q.notify({
        type: 'negative',
        icon: 'error',
        position: 'top-right',
        message: err instanceof Error ? err.message || '保存项目设置失败' : '保存项目设置失败',
        timeout: 5000,
        actions: [{ icon: 'close', color: 'white', round: true }],
      });
    }
  } finally {
    saving.value = false;
  }
}

function wasNotified(err: unknown) {
  return Boolean(err && typeof err === 'object' && '__anycodeNotified' in err);
}
</script>
