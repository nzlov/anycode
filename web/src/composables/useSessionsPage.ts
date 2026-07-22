import { computed, ref } from 'vue';

import { createLatestRequestTracker } from '@/services/sessionEventTimeline';
import {
  listSessions,
  type ListSessionsInput,
  type PageInfo,
  type SessionPage,
} from '@/services/sessions';

interface UseSessionsPageInput extends ListSessionsInput {
  loadAll?: boolean;
}

const defaultPageInfo: PageInfo = {
  page: 1,
  pageSize: 8,
  total: 0,
  nextCursor: '',
};

export function useSessionsPage(defaultInput: UseSessionsPageInput = {}) {
  const rows = ref<SessionPage['items']>([]);
  const pageInfo = ref<PageInfo>({ ...defaultPageInfo });
  const loading = ref(false);
  const filter = ref(defaultInput.filter ?? '');
  const scope = ref(defaultInput.scope ?? '');
  const range = ref(defaultInput.range ?? '');
  const olderThanDays = ref(defaultInput.olderThanDays ?? 0);
  const projectId = ref(defaultInput.projectId ?? '');
  const page = ref(defaultInput.page ?? 1);
  const pageSize = ref(defaultInput.pageSize ?? 8);
  const sort = ref(defaultInput.sort ?? 'updated_at desc');
  const loadRequests = createLatestRequestTracker();

  const input = computed<ListSessionsInput>(() => {
    const value: ListSessionsInput = {
      page: page.value,
      pageSize: pageSize.value,
    };
    if (projectId.value) value.projectId = projectId.value;
    if (scope.value) value.scope = scope.value;
    if (range.value) value.range = range.value;
    if (olderThanDays.value) value.olderThanDays = olderThanDays.value;
    if (filter.value.trim()) value.filter = filter.value.trim();
    if (sort.value) value.sort = sort.value;
    return value;
  });

  async function loadSessions() {
    const requestGeneration = loadRequests.next();
    const requestInput = { ...input.value };
    loading.value = true;
    try {
      if (defaultInput.loadAll) {
        const allItems: SessionPage['items'] = [];
        let currentPage = 1;
        let total = 0;
        let lastPageInfo: PageInfo | null = null;
        let lastPageItemCount = 0;
        do {
          const result = await listSessions({
            ...requestInput,
            page: currentPage,
            pageSize: 100,
          });
          allItems.push(...result.items);
          total = result.pageInfo.total;
          lastPageInfo = result.pageInfo;
          lastPageItemCount = result.items.length;
          currentPage += 1;
        } while (allItems.length < total && lastPageItemCount > 0);
        if (!loadRequests.isCurrent(requestGeneration)) return;
        rows.value = allItems;
        pageInfo.value = lastPageInfo ?? { ...defaultPageInfo };
        return;
      }
      const result = await listSessions(requestInput);
      if (!loadRequests.isCurrent(requestGeneration)) return;
      rows.value = result.items;
      pageInfo.value = result.pageInfo;
    } finally {
      if (loadRequests.isCurrent(requestGeneration)) {
        loading.value = false;
      }
    }
  }

  return {
    rows,
    pageInfo,
    loading,
    filter,
    scope,
    range,
    olderThanDays,
    projectId,
    page,
    pageSize,
    sort,
    loadSessions,
  };
}
