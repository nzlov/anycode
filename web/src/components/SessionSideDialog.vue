<template>
  <q-dialog :model-value="modelValue" @update:model-value="emit('update:modelValue', $event)">
    <q-card class="side-dialog app-content-dialog">
      <q-card-section class="side-dialog__header">
        <q-btn
          v-if="selectedSide"
          flat
          round
          dense
          class="app-icon-btn"
          icon="arrow_back"
          aria-label="返回 Side 列表"
          @click="selectedSideId = ''"
        />
        <div class="side-dialog__title">
          <div class="text-subtitle1 text-weight-bold">Side 临时提问</div>
          <div class="text-caption text-muted">只读运行，不保存到 AnyCode</div>
        </div>
        <q-btn
          flat
          round
          dense
          class="app-icon-btn"
          icon="close"
          aria-label="关闭 Side 窗口"
          v-close-popup
        />
      </q-card-section>
      <q-separator />

      <div v-if="selectedSide" class="side-dialog__detail">
        <div class="side-dialog__events">
          <div v-if="selectedEvents.length" class="side-dialog__event-list">
            <SessionEventMessage
              v-for="event in selectedEvents"
              :key="event.id"
              :event="event"
              :known-user-prompts="[selectedSide.prompt, ...selectedSide.followUps]"
            />
          </div>
          <div v-else class="side-dialog__empty text-muted">
            {{ selectedSide.status === 'running' ? 'Codex 正在读取工作区…' : '暂无事件' }}
          </div>
        </div>
        <SessionThinkingPhrase
          v-if="selectedSide.status === 'running'"
          class="side-dialog__thinking"
          :refresh-key="selectedSide.events.at(-1) ?? null"
        />
        <q-banner v-if="selectedSide.error" rounded class="side-dialog__error">
          {{ selectedSide.error }}
        </q-banner>
        <div class="side-dialog__follow-up">
          <SessionSidePromptInput
            v-model="followUpPrompt"
            label="继续追问"
            :loading="submitting"
            :disabled="selectedSide.status === 'running'"
            @submit="continueSelectedSide"
          />
        </div>
      </div>

      <div v-else-if="sides.length" class="side-dialog__list-wrap">
        <q-list separator class="side-dialog__list app-touch-list">
          <q-item
            v-for="side in sides"
            :key="side.codexSessionId"
            clickable
            @click="openSide(side.codexSessionId)"
          >
            <q-item-section>
              <q-item-label class="side-dialog__prompt">{{ side.prompt }}</q-item-label>
              <q-item-label caption>{{ sideStatusLabel(side.status) }}</q-item-label>
            </q-item-section>
            <q-item-section side>
              <q-btn
                flat
                round
                dense
                class="app-icon-btn"
                icon="close"
                :aria-label="`关闭 Side：${side.prompt}`"
                @click.stop="closeSide(side)"
              >
                <q-tooltip>关闭 Side</q-tooltip>
              </q-btn>
            </q-item-section>
          </q-item>
        </q-list>
        <q-btn
          fab
          color="primary"
          icon="add"
          class="side-dialog__fab"
          aria-label="新建 Side 提问"
          @click="composerOpen = true"
        >
          <q-tooltip>新建 Side 提问</q-tooltip>
        </q-btn>
      </div>

      <q-card-section v-else class="side-dialog__initial-prompt">
        <SessionSidePromptInput v-model="newPrompt" :loading="submitting" @submit="startSide" />
      </q-card-section>
    </q-card>
  </q-dialog>

  <q-dialog v-model="composerOpen">
    <q-card class="side-composer-dialog">
      <q-card-section class="text-subtitle2 text-weight-bold">新建 Side 提问</q-card-section>
      <q-card-section>
        <SessionSidePromptInput v-model="newPrompt" :loading="submitting" @submit="startSide" />
      </q-card-section>
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue';

import SessionEventMessage from '@/components/SessionEventMessage.vue';
import SessionSidePromptInput from '@/components/SessionSidePromptInput.vue';
import SessionThinkingPhrase from '@/components/SessionThinkingPhrase.vue';
import {
  continueSessionSide,
  startSessionSide,
  stopSessionSide,
  subscribeSessionSideEvents,
  type SessionSideRun,
} from '@/services/sessionSides';
import type { TranscriptEvent, TranscriptItem } from '@/services/sessionTimeline';
import { reduceTranscriptEvents } from '@/services/sessionTimelineReducer';

type SideStatus = 'running' | 'completed' | 'failed';

interface SideRecord extends SessionSideRun {
  prompt: string;
  events: TranscriptEvent[];
  status: SideStatus;
  error: string;
  followUps: string[];
  subscription?: { unsubscribe: () => void };
}

const props = defineProps<{ modelValue: boolean; sessionId: string }>();
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>();

const sides = ref<SideRecord[]>([]);
const selectedSideId = ref('');
const newPrompt = ref('');
const followUpPrompt = ref('');
const composerOpen = ref(false);
const submitting = ref(false);
const selectedSide = computed(
  () => sides.value.find((side) => side.codexSessionId === selectedSideId.value) ?? null,
);
const selectedEvents = computed<TranscriptItem[]>(() =>
  reduceTranscriptEvents(selectedSide.value?.events ?? []),
);

async function startSide() {
  const prompt = newPrompt.value.trim();
  if (!prompt || submitting.value) return;
  submitting.value = true;
  try {
    const run = await startSessionSide(props.sessionId, prompt);
    const side: SideRecord = {
      ...run,
      prompt,
      events: [],
      status: 'running',
      error: '',
      followUps: [],
    };
    sides.value.push(side);
    selectedSideId.value = side.codexSessionId;
    newPrompt.value = '';
    composerOpen.value = false;
    subscribeToSide(side);
  } finally {
    submitting.value = false;
  }
}

async function continueSelectedSide() {
  const side = selectedSide.value;
  const prompt = followUpPrompt.value.trim();
  if (!side || !prompt || side.status === 'running' || submitting.value) return;
  submitting.value = true;
  try {
    side.subscription?.unsubscribe();
    const run = await continueSessionSide(props.sessionId, side.codexSessionId, prompt);
    side.processRunId = run.processRunId;
    side.turnId = run.turnId;
    side.status = 'running';
    side.error = '';
    side.followUps.push(prompt);
    followUpPrompt.value = '';
    subscribeToSide(side);
  } finally {
    submitting.value = false;
  }
}

function subscribeToSide(side: SideRecord) {
  side.subscription = subscribeSessionSideEvents(side.processRunId, {
    onData: (event) => side.events.push(event),
    onError: (error) => {
      side.status = 'failed';
      side.error = error.message;
    },
    onClose: ({ completedByServer }) => {
      if (completedByServer && side.status === 'running') side.status = 'completed';
    },
  });
}

function openSide(codexSessionId: string) {
  selectedSideId.value = codexSessionId;
  followUpPrompt.value = '';
}

function closeSide(side: SideRecord) {
  side.subscription?.unsubscribe();
  if (side.status === 'running') void stopSessionSide(side.processRunId).catch(() => undefined);
  sides.value = sides.value.filter((candidate) => candidate !== side);
  if (selectedSideId.value === side.codexSessionId) selectedSideId.value = '';
}

function sideStatusLabel(status: SideStatus) {
  if (status === 'running') return '进行中';
  if (status === 'failed') return '失败';
  return '已完成';
}

onUnmounted(() => {
  for (const side of sides.value) {
    side.subscription?.unsubscribe();
    if (side.status === 'running') void stopSessionSide(side.processRunId).catch(() => undefined);
  }
});
</script>

<style scoped>
.side-dialog.app-content-dialog {
  display: flex;
  width: min(760px, calc(100vw - 24px)) !important;
  max-width: min(760px, calc(100vw - 24px)) !important;
  height: min(760px, calc(100dvh - 24px));
  max-height: min(760px, calc(100dvh - 24px)) !important;
  flex-direction: column;
  overflow: hidden;
}

.side-dialog__header {
  display: flex;
  align-items: center;
  gap: 10px;
}

.side-dialog__title {
  min-width: 0;
  flex: 1 1 auto;
}

.side-dialog__detail,
.side-dialog__list-wrap {
  position: relative;
  min-height: 0;
  flex: 1 1 auto;
}

.side-dialog__detail {
  display: grid;
  grid-template-rows: minmax(0, 1fr) auto auto auto;
}

.side-dialog__events,
.side-dialog__list-wrap {
  overflow-y: auto;
}

.side-dialog__event-list {
  display: grid;
  gap: 10px;
  padding: 12px;
}

.side-dialog__empty,
.side-dialog__initial-prompt,
.side-dialog__follow-up,
.side-dialog__error {
  padding: 16px;
}

.side-dialog__thinking {
  padding: 0 16px 8px;
}

.side-dialog__error {
  color: var(--q-negative);
}

.side-dialog__list {
  padding-bottom: 88px;
}

.side-dialog__prompt {
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

.side-dialog__fab {
  position: absolute;
  right: 20px;
  bottom: 20px;
}

.side-composer-dialog {
  width: min(560px, calc(100vw - 24px));
}

@media (max-width: 599px) {
  .side-dialog.app-content-dialog {
    width: 100vw !important;
    max-width: none !important;
    height: 100dvh;
    max-height: none !important;
    border-radius: 0;
  }
}
</style>
