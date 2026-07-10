import { computed, ref, watch } from 'vue';

import {
  AnyCodeGraphQLError,
  getGraphQLAccessKey,
  verifyGraphQLAccessKey,
  type GraphQLSubscriptionClose,
} from '@/services/graphqlClient';
import {
  createLatestRequestTracker,
  shouldReconnectCardStream,
} from '@/services/sessionEventTimeline';
import {
  listSessions,
  subscribeSessionCardChanged,
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
  const projectId = ref(defaultInput.projectId ?? '');
  const page = ref(defaultInput.page ?? 1);
  const pageSize = ref(defaultInput.pageSize ?? 8);
  const sort = ref(defaultInput.sort ?? 'updated_at desc');
  let liveStopped = true;
  let eventSubscription: { unsubscribe: () => void } | null = null;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  let refreshTimer: ReturnType<typeof setTimeout> | null = null;
  const loadRequests = createLatestRequestTracker();

  const input = computed<ListSessionsInput>(() => {
    const value: ListSessionsInput = {
      page: page.value,
      pageSize: pageSize.value,
    };
    if (projectId.value) value.projectId = projectId.value;
    if (scope.value) value.scope = scope.value;
    if (range.value) value.range = range.value;
    if (filter.value.trim()) value.filter = filter.value.trim();
    if (sort.value) value.sort = sort.value;
    return value;
  });

  watch(projectId, () => {
    if (liveStopped) return;
    restartLiveSubscription();
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

  function startLiveUpdates() {
    liveStopped = false;
    openSubscription();
  }

  function stopLiveUpdates() {
    liveStopped = true;
    eventSubscription?.unsubscribe();
    eventSubscription = null;
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    if (refreshTimer) {
      clearTimeout(refreshTimer);
      refreshTimer = null;
    }
  }

  function openSubscription() {
    eventSubscription?.unsubscribe();
    eventSubscription = subscribeSessionCardChanged(
      projectId.value ? { projectId: projectId.value } : {},
      {
        onData: scheduleRefresh,
        onError: (err) => {
          if (shouldReconnectLiveError(err)) {
            scheduleReconnect();
          }
        },
        onClose: (close) => {
          void handleSubscriptionClose(close);
        },
      },
    );
  }

  async function handleSubscriptionClose(close: GraphQLSubscriptionClose) {
    const reconnect = await shouldReconnectCardStream(close, () =>
      verifyGraphQLAccessKey(getGraphQLAccessKey()),
    );
    if (liveStopped) return;
    if (reconnect) {
      scheduleReconnect();
      return;
    }
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
  }

  function restartLiveSubscription() {
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    openSubscription();
  }

  function scheduleRefresh() {
    if (refreshTimer) return;
    refreshTimer = setTimeout(() => {
      refreshTimer = null;
      void loadSessions();
    }, 300);
  }

  function scheduleReconnect() {
    if (liveStopped || reconnectTimer) return;
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null;
      void reconnectFromSnapshot();
    }, 1500);
  }

  async function reconnectFromSnapshot() {
    if (liveStopped) return;
    await loadSessions();
    if (!liveStopped) {
      openSubscription();
    }
  }

  return {
    rows,
    pageInfo,
    loading,
    filter,
    scope,
    range,
    projectId,
    page,
    pageSize,
    sort,
    loadSessions,
    startLiveUpdates,
    stopLiveUpdates,
  };
}

function shouldReconnectLiveError(err: Error) {
  return !(err instanceof AnyCodeGraphQLError && err.code === 'auth_failed');
}
