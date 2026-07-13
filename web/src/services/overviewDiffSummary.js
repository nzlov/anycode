export const OVERVIEW_DIFF_SUMMARY_INTERVAL_MS = 5000;

const activeStatuses = new Set(['starting', 'running', 'waiting_user', 'stopping']);

export function activeOverviewDiffSessionIds(cards) {
  return cards.filter((card) => activeStatuses.has(card.status)).map((card) => card.id);
}

export function createOverviewDiffSummaryController({
  loadSummaries,
  applySummaries,
  getVisibleCards,
  isPageVisible,
  intervalMs = OVERVIEW_DIFF_SUMMARY_INTERVAL_MS,
  setTimer = setTimeout,
  clearTimer = clearTimeout,
}) {
  const pendingSessionIds = new Set();
  let runningRequest = null;
  let pollTimer = null;
  let polling = false;
  let stopped = false;

  function refresh(sessionIds) {
    if (stopped) return Promise.resolve();
    for (const sessionId of sessionIds) {
      if (sessionId) pendingSessionIds.add(sessionId);
    }
    if (pendingSessionIds.size === 0) return runningRequest ?? Promise.resolve();
    if (!runningRequest) {
      runningRequest = drainRequests().finally(() => {
        runningRequest = null;
      });
    }
    return runningRequest;
  }

  async function drainRequests() {
    let firstError = null;
    while (!stopped && pendingSessionIds.size > 0) {
      const sessionIds = [...pendingSessionIds];
      pendingSessionIds.clear();
      try {
        const summaries = await loadSummaries(sessionIds);
        if (!stopped) applySummaries(sessionIds, summaries);
      } catch (error) {
        firstError ??= error;
      }
    }
    if (firstError) throw firstError;
  }

  function syncPolling() {
    if (pollTimer !== null) {
      clearTimer(pollTimer);
      pollTimer = null;
    }
    if (!polling || stopped || !isPageVisible()) return;
    const activeSessionIds = activeOverviewDiffSessionIds(getVisibleCards());
    if (activeSessionIds.length === 0) return;
    pollTimer = setTimer(async () => {
      pollTimer = null;
      if (!polling || stopped || !isPageVisible()) return;
      try {
        await refresh(activeOverviewDiffSessionIds(getVisibleCards()));
      } catch {
        // The overview keeps the last successful summary when a batch request fails.
      } finally {
        syncPolling();
      }
    }, intervalMs);
  }

  function start() {
    if (stopped) return;
    polling = true;
    syncPolling();
  }

  function stop() {
    stopped = true;
    polling = false;
    pendingSessionIds.clear();
    if (pollTimer !== null) {
      clearTimer(pollTimer);
      pollTimer = null;
    }
  }

  return { refresh, start, stop, syncPolling };
}
