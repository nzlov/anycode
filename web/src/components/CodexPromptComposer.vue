<template>
  <PromptComposer
    :prompt="prompt"
    :files="files"
    :artifacts="artifacts"
    :model="model"
    :effort="effort"
    :permission="permission"
    :fast="fast"
    :title="title"
    :placeholder="placeholder"
    :disabled="disabled"
    :compact="compact"
    :show-badge="showBadge"
    :force-config-menu="forceConfigMenu"
    :readonly-config="readonlyConfig"
    :collapsible="collapsible"
    :collapsed="collapsed"
    :completion-project-id="completionProjectId"
    :completion-session-id="completionSessionId"
    :completion-has-thread="completionHasThread"
    @update:prompt="emit('update:prompt', $event)"
    @update:files="emit('update:files', $event)"
    @update:artifacts="emit('update:artifacts', $event)"
    @update:model="emit('update:model', $event)"
    @update:effort="emit('update:effort', $event)"
    @update:permission="emit('update:permission', $event)"
    @update:fast="emit('update:fast', $event)"
    @update:collapsed="emit('update:collapsed', $event)"
    @submit="emit('submit')"
  >
    <template #quick-actions>
      <q-btn
        flat
        :round="compact"
        :no-caps="!compact"
        :class="compact ? 'quick-reply-btn app-icon-btn' : 'quick-reply-btn app-command-btn'"
        icon="bolt"
        :label="compact ? undefined : '快捷回复'"
        :aria-label="compact ? '快捷回复' : undefined"
        :disable="disabled"
      >
        <q-tooltip v-if="compact">快捷回复</q-tooltip>
        <q-menu
          class="quick-reply-menu"
          anchor="top right"
          self="bottom right"
          @before-show="refreshQuickCommands"
        >
          <q-linear-progress v-if="quickCommandsLoading" indeterminate color="primary" />
          <q-list v-if="quickCommands.length" dense class="app-touch-list">
            <q-item
              v-for="command in quickCommands"
              :key="command.id"
              v-close-popup
              clickable
              :disable="quickCommandsLoading"
              @click="applyQuickCommand(command.content)"
            >
              <q-item-section>{{ command.content }}</q-item-section>
            </q-item>
          </q-list>
          <div v-else class="quick-reply-menu__empty">
            <q-spinner v-if="quickCommandsLoading" color="primary" size="20px" />
            <template v-else-if="quickCommandsError">加载失败</template>
            <template v-else>暂无快捷指令</template>
          </div>
          <div
            v-if="quickCommandsError || quickCommandPageMax > 1"
            class="quick-reply-menu__footer"
          >
            <span v-if="quickCommandsError" class="text-negative">{{ quickCommandsError }}</span>
            <AppPagination
              v-if="quickCommandPageMax > 1"
              :model-value="quickCommandsPageInfo.page"
              :max="quickCommandPageMax"
              :disabled="quickCommandsLoading"
              @update:model-value="changeQuickCommandPage"
            />
            <q-btn
              v-if="quickCommandsError"
              flat
              round
              dense
              class="app-icon-btn"
              icon="refresh"
              aria-label="重试加载快捷指令"
              @click="refreshQuickCommands"
            >
              <q-tooltip>重试</q-tooltip>
            </q-btn>
          </div>
        </q-menu>
      </q-btn>
    </template>
    <template #actions>
      <slot name="actions" />
    </template>
  </PromptComposer>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue';

import AppPagination from '@/components/AppPagination.vue';
import PromptComposer from '@/components/PromptComposer.vue';
import { normalizeCodexSelection } from '@/components/promptOptions';
import { useQuickCommands } from '@/composables/useQuickCommands';
import { listCodexModelOptions } from '@/services/codexOptions';
import { appendQuickCommand } from '@/services/quickCommandText';
import { hasStoredSessionConfig } from '@/services/newSessionPreferences';
import type { SessionFile } from '@/services/sessionFiles';

const props = withDefaults(
  defineProps<{
    prompt: string;
    files: File[];
    artifacts?: SessionFile[];
    model: string;
    effort: string;
    permission: string;
    fast: boolean;
    title?: string;
    placeholder?: string;
    disabled?: boolean;
    compact?: boolean;
    showBadge?: boolean;
    forceConfigMenu?: boolean;
    readonlyConfig?: boolean;
    collapsible?: boolean;
    collapsed?: boolean;
    completionProjectId?: string;
    completionSessionId?: string;
    completionHasThread?: boolean;
  }>(),
  {
    title: '',
    placeholder: '描述你希望 Codex 完成的任务',
    disabled: false,
    compact: false,
    showBadge: true,
    forceConfigMenu: false,
    readonlyConfig: false,
    collapsible: false,
    collapsed: false,
    completionProjectId: '',
    completionSessionId: '',
    completionHasThread: false,
    artifacts: () => [],
  },
);

const emit = defineEmits<{
  'update:prompt': [value: string];
  'update:files': [value: File[]];
  'update:artifacts': [value: SessionFile[]];
  'update:model': [value: string];
  'update:effort': [value: string];
  'update:permission': [value: string];
  'update:fast': [value: boolean];
  'update:collapsed': [value: boolean];
  submit: [];
}>();

const {
  quickCommands,
  quickCommandsLoading,
  quickCommandsError,
  quickCommandsPageInfo,
  loadQuickCommands,
} = useQuickCommands();
const quickCommandPageMax = computed(() =>
  Math.max(1, Math.ceil(quickCommandsPageInfo.value.total / quickCommandsPageInfo.value.pageSize)),
);

function applyQuickCommand(command: string) {
  emit('update:prompt', appendQuickCommand(props.prompt, command));
  if (props.collapsible) emit('update:collapsed', false);
}

function refreshQuickCommands() {
  void loadQuickCommands({ force: true, page: 1 }).catch(() => undefined);
}

function changeQuickCommandPage(page: number) {
  void loadQuickCommands({ force: true, page }).catch(() => undefined);
}

async function initializeCodexConfig() {
  try {
    const options = await listCodexModelOptions();
    const normalized = normalizeCodexSelection(options, props.model, props.effort);
    if (normalized.model !== props.model) emit('update:model', normalized.model);
    if (normalized.effort !== props.effort) emit('update:effort', normalized.effort);
  } catch {
    // graphqlFetch reports the failure; opening the selector retries the request.
  }
}

onMounted(() => {
  if (!hasStoredSessionConfig()) void initializeCodexConfig();
});
</script>
