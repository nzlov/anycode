<template>
  <q-btn
    :flat="!fullWidth"
    :round="!fullWidth"
    :dense="!fullWidth"
    :outline="fullWidth"
    :class="fullWidth ? 'session-fork-button--full-width q-mt-md app-command-btn' : 'app-icon-btn'"
    color="primary"
    icon="call_split"
    :label="fullWidth ? 'Fork 卡片' : undefined"
    no-caps
    aria-label="Fork 卡片"
    @click.stop="openDialog"
  >
    <q-tooltip v-if="!fullWidth">Fork 卡片</q-tooltip>
  </q-btn>

  <q-dialog v-model="dialogOpen" :persistent="creating">
    <q-card class="session-fork-dialog">
      <q-card-section>
        <div class="text-h6">Fork 卡片</div>
        <div v-if="projectIsGit" class="text-caption text-muted q-mt-xs">
          新卡片继承当前 Codex 上下文；Git 工作区从当前分支 HEAD 创建，未提交修改不会复制。
        </div>
        <div v-else class="text-caption text-muted q-mt-xs">新卡片继承当前 Codex 上下文。</div>
      </q-card-section>

      <q-card-section class="q-pt-none">
        <q-input
          ref="requirementInput"
          v-model="requirement"
          outlined
          autofocus
          autogrow
          type="textarea"
          label="新需求"
          :disable="creating"
          @keydown.ctrl.enter.prevent="createFork"
          @keydown.meta.enter.prevent="createFork"
        />
      </q-card-section>

      <q-card-actions align="right">
        <q-btn flat no-caps label="取消" :disable="creating" v-close-popup />
        <q-btn
          unelevated
          no-caps
          color="primary"
          label="创建并执行"
          :loading="creating"
          :disable="!requirement.trim()"
          @click="createFork"
        />
      </q-card-actions>
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
import { nextTick, ref } from 'vue';
import { useRouter } from 'vue-router';

import { forkSession } from '@/services/sessions';

const props = withDefaults(
  defineProps<{
    sourceSessionId: string;
    projectIsGit?: boolean;
    fullWidth?: boolean;
    stayOnPage?: boolean;
  }>(),
  { fullWidth: false, stayOnPage: false },
);
const emit = defineEmits<{
  forked: [sessionId: string];
}>();
const router = useRouter();
const dialogOpen = ref(false);
const creating = ref(false);
const requirement = ref('');
const requirementInput = ref<{ focus: () => void } | null>(null);

async function openDialog() {
  requirement.value = '';
  dialogOpen.value = true;
  await nextTick();
  requirementInput.value?.focus();
}

async function createFork() {
  const nextRequirement = requirement.value.trim();
  if (!nextRequirement || creating.value) return;
  creating.value = true;
  try {
    const forked = await forkSession({
      sourceSessionId: props.sourceSessionId,
      requirement: nextRequirement,
    });
    dialogOpen.value = false;
    emit('forked', forked.id);
    if (!props.stayOnPage) {
      await router.push({ name: 'session-detail', params: { id: forked.id } });
    }
  } finally {
    creating.value = false;
  }
}
</script>

<style scoped>
.session-fork-dialog {
  width: min(560px, calc(100vw - 32px));
}

.session-fork-button--full-width {
  width: auto;
  min-width: 0;
  flex: 1 1 0;
}
</style>
