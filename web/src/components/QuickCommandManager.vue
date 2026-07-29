<template>
  <div class="quick-command-manager">
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
      <div v-if="adding || editingCommandId" class="quick-command-editor">
        <q-input
          ref="commandInputRef"
          v-model="draftCommand"
          outlined
          autogrow
          :label="editingCommandId ? '修改快捷指令' : '快捷指令'"
          :disable="saving"
          @keyup.ctrl.enter="saveCommand"
        />
        <div class="quick-command-editor__actions">
          <q-btn
            flat
            round
            class="app-icon-btn"
            icon="close"
            :aria-label="editingCommandId ? '取消修改' : '取消新增'"
            :disable="saving"
            @click="cancelEditor"
          >
            <q-tooltip>取消</q-tooltip>
          </q-btn>
          <q-btn
            unelevated
            round
            color="primary"
            class="app-icon-btn app-on-primary"
            icon="check"
            :aria-label="editingCommandId ? '保存快捷指令修改' : '保存快捷指令'"
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
      <q-item v-for="command in quickCommands" :key="command.id" :disable="quickCommandsLoading">
        <q-item-section>
          <q-item-label class="quick-command-text">{{ command.content }}</q-item-label>
        </q-item-section>
        <q-item-section side>
          <div class="quick-command-item__actions">
            <q-btn
              flat
              round
              dense
              class="app-icon-btn"
              icon="edit"
              :aria-label="`修改快捷指令：${command.content}`"
              :disable="quickCommandsLoading || quickCommandsMutating > 0 || saving"
              @click="startEdit(command)"
            >
              <q-tooltip>修改</q-tooltip>
            </q-btn>
            <q-btn
              flat
              round
              dense
              class="app-icon-btn"
              color="negative"
              icon="delete_outline"
              :aria-label="`删除快捷指令：${command.content}`"
              :loading="deletingCommandIds.includes(command.id)"
              :disable="quickCommandsLoading || quickCommandsMutating > 0 || saving"
              @click="removeCommand(command.id)"
            >
              <q-tooltip>删除</q-tooltip>
            </q-btn>
          </div>
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
      v-if="!adding && !editingCommandId"
      fab
      color="primary"
      class="global-settings-add-fab app-on-primary"
      icon="add"
      aria-label="新增快捷指令"
      :disable="quickCommandsLoading || quickCommandsMutating > 0"
      @click="startAdd"
    >
      <q-tooltip>新增快捷指令</q-tooltip>
    </q-btn>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue';

import AppPagination from '@/components/AppPagination.vue';
import { useQuickCommands } from '@/composables/useQuickCommands';

const props = defineProps<{
  projectId?: string;
}>();

const {
  quickCommands,
  quickCommandsLoading,
  quickCommandsMutating,
  quickCommandsError,
  quickCommandsPageInfo,
  loadQuickCommands,
  addQuickCommand,
  editQuickCommand,
  deleteQuickCommand,
} = useQuickCommands({ projectId: () => props.projectId });
const adding = ref(false);
const editingCommandId = ref('');
const draftCommand = ref('');
const saving = ref(false);
const deletingCommandIds = ref<string[]>([]);
const commandInputRef = ref<{ focus: () => void } | null>(null);
const quickCommandPageMax = computed(() =>
  Math.max(1, Math.ceil(quickCommandsPageInfo.value.total / quickCommandsPageInfo.value.pageSize)),
);

function startAdd() {
  editingCommandId.value = '';
  adding.value = true;
  void nextTick(() => commandInputRef.value?.focus());
}

function startEdit(command: { id: string; content: string }) {
  adding.value = false;
  editingCommandId.value = command.id;
  draftCommand.value = command.content;
  void nextTick(() => commandInputRef.value?.focus());
}

function cancelEditor() {
  adding.value = false;
  editingCommandId.value = '';
  draftCommand.value = '';
}

async function saveCommand() {
  if (!draftCommand.value.trim()) return;
  saving.value = true;
  try {
    if (editingCommandId.value) {
      await editQuickCommand(editingCommandId.value, draftCommand.value);
    } else {
      await addQuickCommand(draftCommand.value);
    }
    cancelEditor();
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

onMounted(refreshQuickCommands);
</script>
