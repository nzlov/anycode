<template>
  <section class="workflow-approval-panel" aria-label="人工审批">
    <div class="workflow-approval-panel__header">
      <div class="text-subtitle2 text-weight-bold">人工审批</div>
      <q-badge rounded color="warning" text-color="dark" label="待审批" />
    </div>

    <q-banner v-if="!contextAvailable" dense rounded class="state-banner bg-negative text-white">
      未找到当前审批上下文，请刷新后重试
    </q-banner>

    <q-input
      v-if="mode === 'reject'"
      v-model="rejectPrompt"
      outlined
      autogrow
      autofocus
      label="拒绝时的新提示词"
      :disable="submitting"
    />

    <div class="workflow-approval-panel__actions">
      <template v-if="mode === 'reject'">
        <q-btn
          flat
          icon="arrow_back"
          label="返回"
          :disable="submitting || submittedDecision !== ''"
          @click="returnToDecision"
        />
        <q-btn
          unelevated
          color="negative"
          icon="close"
          label="确认拒绝"
          :loading="submitting && submittedDecision === 'reject'"
          :disable="
            submitting ||
            submittedDecision !== '' ||
            !contextAvailable ||
            rejectPrompt.trim() === ''
          "
          @click="submitReject"
        />
      </template>
      <template v-else>
        <q-btn
          flat
          color="negative"
          icon="close"
          label="拒绝"
          :disable="submitting || submittedDecision !== '' || !contextAvailable"
          @click="beginReject"
        />
        <q-btn
          unelevated
          color="primary"
          icon="check"
          label="通过"
          :loading="submitting && submittedDecision === 'approve'"
          :disable="submitting || submittedDecision !== '' || !contextAvailable"
          @click="submitApprove"
        />
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue';

const props = withDefaults(
  defineProps<{
    contextAvailable: boolean;
    submitting?: boolean;
  }>(),
  { submitting: false },
);

const emit = defineEmits<{
  submit: [approved: boolean, comment: string];
}>();

const mode = ref<'decision' | 'reject'>('decision');
const rejectPrompt = ref('');
const submittedDecision = ref<'approve' | 'reject' | ''>('');

watch(
  () => props.submitting,
  (submitting) => {
    if (!submitting) submittedDecision.value = '';
  },
);

function beginReject() {
  if (!props.contextAvailable || props.submitting) return;
  mode.value = 'reject';
}

function returnToDecision() {
  if (props.submitting) return;
  mode.value = 'decision';
}

function submitReject() {
  const comment = rejectPrompt.value.trim();
  if (
    !props.contextAvailable ||
    props.submitting ||
    submittedDecision.value !== '' ||
    comment === ''
  ) {
    return;
  }
  submittedDecision.value = 'reject';
  emit('submit', false, comment);
}

function submitApprove() {
  if (!props.contextAvailable || props.submitting || submittedDecision.value !== '') return;
  submittedDecision.value = 'approve';
  emit('submit', true, '');
}
</script>

<style scoped>
.workflow-approval-panel {
  display: grid;
  min-width: 0;
  gap: 12px;
  padding: 12px 16px;
  background: var(--ac-surface-raised);
}

.workflow-approval-panel__header,
.workflow-approval-panel__actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.workflow-approval-panel__actions {
  justify-content: flex-end;
}

@media (max-width: 599.98px) {
  .workflow-approval-panel {
    padding: 12px;
  }

  .workflow-approval-panel__actions {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  }
}
</style>
