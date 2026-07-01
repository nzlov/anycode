import { computed, ref } from 'vue';

import {
  listSessions,
  type ListSessionsInput,
  type PageInfo,
  type SessionPage,
} from '@/services/sessions';

const defaultPageInfo: PageInfo = {
  page: 1,
  pageSize: 8,
  total: 0,
  nextCursor: '',
};

export function useSessionsPage(defaultInput: ListSessionsInput = {}) {
  const rows = ref<SessionPage['items']>([]);
  const pageInfo = ref<PageInfo>({ ...defaultPageInfo });
  const loading = ref(false);
  const filter = ref(defaultInput.filter ?? '');
  const scope = ref(defaultInput.scope ?? '');
  const range = ref(defaultInput.range ?? '');
  const page = ref(defaultInput.page ?? 1);
  const pageSize = ref(defaultInput.pageSize ?? 8);
  const sort = ref(defaultInput.sort ?? 'updated_at desc');

  const input = computed<ListSessionsInput>(() => {
    const value: ListSessionsInput = {
      page: page.value,
      pageSize: pageSize.value,
    };
    if (defaultInput.projectId) value.projectId = defaultInput.projectId;
    if (scope.value) value.scope = scope.value;
    if (range.value) value.range = range.value;
    if (filter.value.trim()) value.filter = filter.value.trim();
    if (sort.value) value.sort = sort.value;
    return value;
  });

  async function loadSessions() {
    loading.value = true;
    try {
      const result = await listSessions(input.value);
      rows.value = result.items;
      pageInfo.value = result.pageInfo;
    } finally {
      loading.value = false;
    }
  }

  return {
    rows,
    pageInfo,
    loading,
    filter,
    scope,
    range,
    page,
    pageSize,
    sort,
    loadSessions,
  };
}
