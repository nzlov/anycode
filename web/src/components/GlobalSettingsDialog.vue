<template>
  <q-dialog
    :model-value="modelValue"
    :maximized="$q.screen.lt.sm"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <q-card class="global-settings-dialog app-content-dialog">
      <q-card-section class="global-settings-header row items-center">
        <div class="text-subtitle1 text-weight-bold">全局设置</div>
        <q-space />
        <q-btn v-close-popup flat round dense class="app-icon-btn" icon="close" aria-label="关闭">
          <q-tooltip>关闭</q-tooltip>
        </q-btn>
      </q-card-section>

      <q-separator />

      <div class="global-settings-grid">
        <nav class="global-settings-nav" aria-label="全局设置分类">
          <q-list padding>
            <q-item clickable active active-class="global-settings-nav__active">
              <q-item-section avatar>
                <q-icon name="bolt" />
              </q-item-section>
              <q-item-section>快捷指令</q-item-section>
            </q-item>
          </q-list>
        </nav>

        <section class="global-settings-panel">
          <div class="global-settings-panel__header">
            <div class="text-subtitle2 text-weight-bold">快捷指令</div>
          </div>

          <q-banner v-if="quickCommandsError" dense class="quick-command-error">
            <template #avatar>
              <q-icon name="error_outline" color="negative" />
            </template>
            {{ quickCommandsError }}
            <template #action>
              <q-btn
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
            </template>
          </q-banner>

          <q-slide-transition>
            <div v-if="adding" class="quick-command-editor">
              <q-input
                ref="commandInputRef"
                v-model="draftCommand"
                outlined
                autogrow
                label="快捷指令"
                :disable="saving"
                @keyup.ctrl.enter="saveCommand"
              />
              <div class="quick-command-editor__actions">
                <q-btn
                  flat
                  round
                  class="app-icon-btn"
                  icon="close"
                  aria-label="取消新增"
                  :disable="saving"
                  @click="cancelAdd"
                >
                  <q-tooltip>取消</q-tooltip>
                </q-btn>
                <q-btn
                  unelevated
                  round
                  class="app-icon-btn"
                  color="positive"
                  text-color="dark"
                  icon="check"
                  aria-label="保存快捷指令"
                  :loading="saving"
                  :disable="saving || !draftCommand.trim()"
                  @click="saveCommand"
                >
                  <q-tooltip>保存</q-tooltip>
                </q-btn>
              </div>
            </div>
          </q-slide-transition>

          <q-linear-progress
            v-if="quickCommandsLoading && quickCommands.length"
            indeterminate
            color="primary"
          />
          <q-list v-if="quickCommands.length" separator class="quick-command-list">
            <q-item
              v-for="command in quickCommands"
              :key="command.id"
              :disable="quickCommandsLoading"
            >
              <q-item-section>
                <q-item-label class="quick-command-text">{{ command.content }}</q-item-label>
              </q-item-section>
              <q-item-section side>
                <q-btn
                  flat
                  round
                  dense
                  class="app-icon-btn"
                  color="negative"
                  icon="delete_outline"
                  :aria-label="`删除快捷指令：${command.content}`"
                  :loading="deletingCommandIds.includes(command.id)"
                  :disable="quickCommandsLoading || quickCommandsMutating > 0"
                  @click="removeCommand(command.id)"
                >
                  <q-tooltip>删除</q-tooltip>
                </q-btn>
              </q-item-section>
            </q-item>
          </q-list>
          <div v-else-if="!quickCommandsError" class="global-settings-empty">
            <q-spinner v-if="quickCommandsLoading" color="primary" size="24px" />
            <template v-else>暂无快捷指令</template>
          </div>

          <AppPagination
            v-if="quickCommandPageMax > 1"
            :model-value="quickCommandsPageInfo.page"
            :max="quickCommandPageMax"
            :disabled="quickCommandsLoading || quickCommandsMutating > 0"
            class="quick-command-pagination"
            @update:model-value="changeQuickCommandPage"
          />

          <q-btn
            fab
            class="global-settings-add-fab"
            color="positive"
            text-color="dark"
            icon="add"
            aria-label="新增快捷指令"
            :disable="adding || quickCommandsLoading || quickCommandsMutating > 0"
            @click="startAdd"
          >
            <q-tooltip>新增快捷指令</q-tooltip>
          </q-btn>
        </section>
      </div>
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue';

import AppPagination from '@/components/AppPagination.vue';
import { useQuickCommands } from '@/composables/useQuickCommands';

const props = defineProps<{
  modelValue: boolean;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
}>();

const {
  quickCommands,
  quickCommandsLoading,
  quickCommandsMutating,
  quickCommandsError,
  quickCommandsPageInfo,
  loadQuickCommands,
  addQuickCommand,
  deleteQuickCommand,
} = useQuickCommands();
const adding = ref(false);
const draftCommand = ref('');
const saving = ref(false);
const deletingCommandIds = ref<string[]>([]);
const commandInputRef = ref<{ focus: () => void } | null>(null);
const quickCommandPageMax = computed(() =>
  Math.max(1, Math.ceil(quickCommandsPageInfo.value.total / quickCommandsPageInfo.value.pageSize)),
);

function startAdd() {
  adding.value = true;
  void nextTick(() => commandInputRef.value?.focus());
}

function cancelAdd() {
  adding.value = false;
  draftCommand.value = '';
}

async function saveCommand() {
  if (!draftCommand.value.trim()) return;
  saving.value = true;
  try {
    await addQuickCommand(draftCommand.value);
    cancelAdd();
  } catch {
    return;
  } finally {
    saving.value = false;
  }
}

async function removeCommand(id: string) {
  deletingCommandIds.value = [...deletingCommandIds.value, id];
  try {
    await deleteQuickCommand(id);
  } catch {
    return;
  } finally {
    deletingCommandIds.value = deletingCommandIds.value.filter((commandID) => commandID !== id);
  }
}

function refreshQuickCommands() {
  void loadQuickCommands({ force: true }).catch(() => undefined);
}

function changeQuickCommandPage(page: number) {
  void loadQuickCommands({ force: true, page }).catch(() => undefined);
}

onMounted(() => {
  void loadQuickCommands().catch(() => undefined);
});

watch(
  () => props.modelValue,
  (open) => {
    if (open) refreshQuickCommands();
  },
);
</script>
