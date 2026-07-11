import { ref } from 'vue';

import {
  prependQuickCommand,
  removeQuickCommandById,
  shouldApplyQuickCommandSnapshot,
} from '@/services/quickCommandState';
import {
  createQuickCommand,
  deleteQuickCommand as removeQuickCommand,
  listQuickCommands,
  type QuickCommand,
} from '@/services/quickCommands';

export function useQuickCommands() {
  const quickCommands = ref<QuickCommand[]>([]);
  const quickCommandsLoading = ref(false);
  const quickCommandsMutating = ref(0);
  const quickCommandsError = ref('');
  const quickCommandsPageInfo = ref({ page: 1, pageSize: 20, total: 0, nextCursor: '' });
  let loaded = false;
  let loadedPage = 0;
  let loadedPageSize = 0;
  let latestRequestId = 0;
  let mutationVersion = 0;
  let refreshNeeded = false;
  let refreshPage = 1;

  async function loadQuickCommands(
    options: { force?: boolean; page?: number; pageSize?: number } = {},
  ) {
    const page = Math.max(1, options.page ?? quickCommandsPageInfo.value.page);
    const pageSize = Math.max(1, options.pageSize ?? quickCommandsPageInfo.value.pageSize);
    if (quickCommandsMutating.value > 0) return;
    if (loaded && !options.force && loadedPage === page && loadedPageSize === pageSize) return;
    const requestId = latestRequestId + 1;
    latestRequestId = requestId;
    const loadMutationVersion = mutationVersion;
    quickCommandsLoading.value = true;
    quickCommandsError.value = '';
    try {
      const result = await listQuickCommands({ page, pageSize });
      if (
        !shouldApplyQuickCommandSnapshot(
          requestId,
          latestRequestId,
          loadMutationVersion,
          mutationVersion,
        )
      ) {
        return;
      }
      quickCommands.value = result.items;
      quickCommandsPageInfo.value = result.pageInfo;
      loaded = true;
      loadedPage = result.pageInfo.page;
      loadedPageSize = result.pageInfo.pageSize;
    } catch (error) {
      if (requestId === latestRequestId) {
        quickCommandsError.value = errorMessage(error, '读取快捷指令失败');
      }
      throw error;
    } finally {
      if (requestId === latestRequestId) {
        quickCommandsLoading.value = false;
      }
    }
  }

  async function addQuickCommand(content: string) {
    quickCommandsError.value = '';
    beginMutation();
    let command: QuickCommand;
    try {
      command = await createQuickCommand(content);
    } catch (error) {
      const message = errorMessage(error, '新增快捷指令失败');
      await finishMutation(message);
      throw error;
    }
    quickCommands.value =
      quickCommandsPageInfo.value.page === 1
        ? prependQuickCommand(quickCommands.value, command, quickCommandsPageInfo.value.pageSize)
        : [command];
    quickCommandsPageInfo.value = {
      ...quickCommandsPageInfo.value,
      page: 1,
      total: quickCommandsPageInfo.value.total + 1,
    };
    loadedPage = 1;
    loadedPageSize = quickCommandsPageInfo.value.pageSize;
    markRefreshNeeded(1);
    await finishMutation();
    return command;
  }

  async function deleteQuickCommand(id: string) {
    quickCommandsError.value = '';
    beginMutation();
    try {
      await removeQuickCommand(id);
    } catch (error) {
      const message = errorMessage(error, '删除快捷指令失败');
      await finishMutation(message);
      throw error;
    }
    quickCommands.value = removeQuickCommandById(quickCommands.value, id);
    const total = Math.max(0, quickCommandsPageInfo.value.total - 1);
    const maxPage = Math.max(1, Math.ceil(total / quickCommandsPageInfo.value.pageSize));
    const page = Math.min(quickCommandsPageInfo.value.page, maxPage);
    quickCommandsPageInfo.value = { ...quickCommandsPageInfo.value, page, total };
    loadedPage = page;
    loadedPageSize = quickCommandsPageInfo.value.pageSize;
    markRefreshNeeded(page);
    await finishMutation();
  }

  function beginMutation() {
    mutationVersion += 1;
    quickCommandsMutating.value += 1;
  }

  function markRefreshNeeded(page: number) {
    refreshNeeded = true;
    refreshPage = page;
  }

  async function finishMutation(preserveError = '') {
    quickCommandsMutating.value = Math.max(0, quickCommandsMutating.value - 1);
    if (quickCommandsMutating.value === 0 && refreshNeeded) {
      refreshNeeded = false;
      await loadQuickCommands({ force: true, page: refreshPage }).catch(() => undefined);
    }
    if (preserveError) quickCommandsError.value = preserveError;
  }

  return {
    quickCommands,
    quickCommandsLoading,
    quickCommandsMutating,
    quickCommandsError,
    quickCommandsPageInfo,
    loadQuickCommands,
    addQuickCommand,
    deleteQuickCommand,
  };
}

function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error ? error.message || fallback : fallback;
}
