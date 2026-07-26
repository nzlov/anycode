<template>
  <component
    :is="page ? 'div' : QDialog"
    :model-value="page ? undefined : modelValue"
    :persistent="saving"
    aria-label="项目设置"
    @update:model-value="emitModel"
  >
    <q-card class="project-settings-dialog app-content-dialog">
      <q-card-section class="row items-center q-pb-sm">
        <div class="text-subtitle1 text-weight-bold">项目设置</div>
        <q-space />
        <q-btn
          flat
          round
          dense
          class="app-icon-btn"
          icon="close"
          aria-label="关闭"
          :disable="saving"
          @click="emitModel(false)"
        >
          <q-tooltip>关闭</q-tooltip>
        </q-btn>
      </q-card-section>

      <q-separator />

      <q-tabs v-model="activeSection" dense align="left" no-caps class="global-settings-tabs">
        <q-tab name="general" icon="tune" label="常规" />
        <q-tab name="quick_commands" icon="bolt" label="快捷指令" />
      </q-tabs>

      <q-card-section v-if="activeSection === 'general'" class="project-settings-dialog__body">
        <q-input
          v-model="worktreeInitCommand"
          outlined
          type="textarea"
          label="工作树初始化命令"
          :rows="10"
        />
      </q-card-section>
      <q-card-section
        v-else
        class="project-settings-dialog__body project-settings-dialog__commands"
      >
        <QuickCommandManager v-if="project" :project-id="project.id" />
        <q-inner-loading v-else showing color="primary" />
      </q-card-section>

      <q-separator v-if="activeSection === 'general'" />

      <q-card-actions v-if="activeSection === 'general'" align="right">
        <q-btn
          flat
          round
          class="app-icon-btn"
          icon="close"
          color="primary"
          aria-label="取消"
          :disable="saving"
          @click="emitModel(false)"
        >
          <q-tooltip>取消</q-tooltip>
        </q-btn>
        <q-btn
          unelevated
          color="primary"
          class="app-command-btn app-on-primary"
          icon="save"
          label="保存"
          no-caps
          :loading="saving"
          :disable="!project"
          @click="save"
        />
      </q-card-actions>
    </q-card>
  </component>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue';
import { QDialog, useQuasar } from 'quasar';

import QuickCommandManager from '@/components/QuickCommandManager.vue';
import { useProjects } from '@/composables/useProjects';
import type { ProjectSummary } from '@/services/projects';

const props = defineProps<{
  modelValue: boolean;
  project: ProjectSummary | null;
  page?: boolean;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
}>();

const $q = useQuasar();
const { updateProjectSettingsById } = useProjects();
const activeSection = ref<'general' | 'quick_commands'>('general');
const worktreeInitCommand = ref('');
const saving = ref(false);

watch(
  () => [props.modelValue, props.project?.id] as const,
  ([open]) => {
    if (open) {
      activeSection.value = 'general';
      worktreeInitCommand.value = props.project?.worktreeInitCommand ?? '';
    }
  },
  { immediate: true },
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
