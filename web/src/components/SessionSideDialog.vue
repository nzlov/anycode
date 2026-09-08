<template>
  <q-dialog
    :model-value="modelValue"
    @update:model-value="emit('update:modelValue', $event)"
    @show="scrollEventsToBottom(true)"
  >
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
        <div ref="eventsBodyRef" class="side-dialog__events" @scroll="updateEventScroll">
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
            v-model="selectedSide.draft"
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
        <SessionSidePromptInput v-model="newMessage" :loading="submitting" @submit="startSide" />
      </q-card-section>
    </q-card>
  </q-dialog>

  <q-dialog v-model="composerOpen">
    <q-card class="side-composer-dialog">
      <q-card-section class="text-subtitle2 text-weight-bold">新建 Side 提问</q-card-section>
      <q-card-section>
        <SessionSidePromptInput v-model="newMessage" :loading="submitting" @submit="startSide" />
      </q-card-section>
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from 'vue';

import { useEventStreamScroll } from '@/composables/useEventStreamScroll';

import SessionEventMessage from '@/components/SessionEventMessage.vue';
import SessionSidePromptInput from '@/components/SessionSidePromptInput.vue';
import SessionThinkingPhrase from '@/components/SessionThinkingPhrase.vue';
import {
  continueSessionSide,
  startSessionSide,
  stopSessionSide,
  subscribeSessionSideEvents,
  type SessionSideRun,
  type SessionSideConfig,
  type SessionSideMessage,
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
  draft: SessionSideMessage;
  subscription?: { unsubscribe: () => void };
}

const props = defineProps<{ modelValue: boolean; sessionId: string; config: SessionSideConfig }>();
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>();

const sides = ref<SideRecord[]>([]);
const selectedSideId = ref('');
const newMessage = ref<SessionSideMessage>({ prompt: '', files: [], config: { ...props.config } });
const eventsBodyRef = ref<HTMLElement | null>(null);
const { updateEventScroll, scrollEventsToBottom, followLatestEvent } =
  useEventStreamScroll(eventsBodyRef);
let disposed = false;
const composerOpen = ref(false);
const submitting = ref(false);
const selectedSide = computed(
  () => sides.value.find((side) => side.codexSessionId === selectedSideId.value) ?? null,
);
const selectedEvents = computed<TranscriptItem[]>(() =>
  reduceTranscriptEvents(selectedSide.value?.events ?? []),
);

watch(() => selectedSide.value?.events.at(-1), followLatestEvent);
watch(selectedSideId, () => {
  void scrollEventsToBottom(true);
});

function messagePrompt(message: SessionSideMessage) {
  return (
    message.prompt.trim() ||
    (message.files.length ? `请查看附件：${message.files.map((file) => file.name).join('、')}` : '')
  );
}

async function startSide() {
  const message = newMessage.value;
  const prompt = messagePrompt(message);
  if (!prompt || submitting.value) return;
  submitting.value = true;
  try {
    const run = await startSessionSide(props.sessionId, { ...message, prompt });
    if (disposed) {
      await stopSessionSide(run.processRunId);
      return;
    }
    const side = reactive<SideRecord>({
      ...run,
      prompt,
      events: [],
      status: 'running',
      error: '',
      followUps: [],
      draft: { prompt: '', files: [], config: { ...message.config } },
    });
    sides.value.push(side);
    selectedSideId.value = side.codexSessionId;
    newMessage.value = { prompt: '', files: [], config: { ...props.config } };
    composerOpen.value = false;
    subscribeToSide(side);
  } catch {
    // The request client displays the error; keep the draft and files for retry.
  } finally {
    submitting.value = false;
  }
}

async function continueSelectedSide() {
  const side = selectedSide.value;
  if (!side || side.status === 'running' || submitting.value) return;
  const message = side.draft;
  const prompt = messagePrompt(message);
  if (!prompt) return;
  submitting.value = true;
  try {
    const run = await continueSessionSide(props.sessionId, side.codexSessionId, {
      ...message,
      prompt,
    });
    if (disposed || !sides.value.includes(side)) {
      await stopSessionSide(run.processRunId);
      return;
    }
    side.subscription?.unsubscribe();
    side.processRunId = run.processRunId;
    side.turnId = run.turnId;
    side.status = 'running';
    side.error = '';
    side.followUps.push(prompt);
    side.draft = { prompt: '', files: [], config: { ...message.config } };
    subscribeToSide(side);
  } catch {
    // The request client displays the error; keep the draft and files for retry.
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
}

function closeSide(side: SideRecord) {
  side.subscription?.unsubscribe();
  void stopSessionSide(side.processRunId).catch(() => undefined);
  sides.value = sides.value.filter((candidate) => candidate !== side);
  if (selectedSideId.value === side.codexSessionId) selectedSideId.value = '';
}

function sideStatusLabel(status: SideStatus) {
  if (status === 'running') return '进行中';
  if (status === 'failed') return '失败';
  return '已完成';
}

onUnmounted(() => {
  disposed = true;
  for (const side of sides.value) {
    side.subscription?.unsubscribe();
    void stopSessionSide(side.processRunId).catch(() => undefined);
  }
});
</script>

<style scoped>
.side-dialog.app-content-dialog {
  display: flex;
  width: min(760px, calc(100vw - 24px)) !important;
  max-width: min(760px, calc(100vw - 24px)) !important;
  height: min(760px, calc(100dvh - 48px));
  max-height: min(760px, calc(100dvh - 48px)) !important;
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
  overflow: hidden;
}

.side-dialog__events,
.side-dialog__list-wrap {
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
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

.side-dialog__initial-prompt,
.side-dialog__follow-up {
  min-height: 0;
  max-height: min(420px, 55dvh);
  overflow-y: auto;
}

.side-dialog__thinking {
  padding: 0 16px 8px;
}

.side-dialog__follow-up {
  flex: 0 0 auto;
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

.side-composer-dialog > .q-card__section:last-child {
  min-height: 0;
  overflow-y: auto;
}

.side-composer-dialog {
  display: flex;
  width: min(560px, calc(100vw - 24px));
  max-height: calc(100dvh - 48px) !important;
  flex-direction: column;
  overflow: hidden;
}

@media (max-width: 599px) {
  .side-dialog.app-content-dialog {
    width: calc(100vw - 24px) !important;
    max-width: calc(100vw - 24px) !important;
    height: calc(100dvh - 48px);
    max-height: calc(100dvh - 48px) !important;
    border-radius: 0;
  }
}
</style>
