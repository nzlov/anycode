import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

test('session detail event stream uses transcript events instead of database prompts', () => {
  const source = readFileSync(
    new URL('../src/pages/SessionDetailPage.vue', import.meta.url),
    'utf8',
  );
  const streamBlock = source.slice(
    source.indexOf('const streamEntries'),
    source.indexOf('const composerAction'),
  );

  assert.equal(streamBlock.includes('session.value.summary'), false);
  assert.equal(streamBlock.includes('session.value.promptAppends'), false);
  assert.equal(streamBlock.includes('session-input-'), false);
  assert.equal(streamBlock.includes('prompt-append-'), false);
});

test('session event presentation moves usage out of the event list into session info', () => {
  const serviceSource = readFileSync(
    new URL('../src/services/sessions.ts', import.meta.url),
    'utf8',
  );
  const componentSource = readFileSync(
    new URL('../src/components/SessionEventMessage.vue', import.meta.url),
    'utf8',
  );
  const toolComponentSource = readFileSync(
    new URL('../src/components/SessionToolEvent.vue', import.meta.url),
    'utf8',
  );

  assert.match(serviceSource, /itemType === 'user_message'/);
  assert.match(serviceSource, /stringPayload\(item, 'call_id'\)[\s\S]*stringPayload\(item, 'id'\)/);
  assert.match(serviceSource, /codexType === 'task\.started'/);
  assert.match(serviceSource, /codexType === 'task\.completed'/);
  assert.match(serviceSource, /codexType === 'turn\.aborted'/);
  assert.match(serviceSource, /codexType === 'token_count'/);
  assert.match(componentSource, /SessionToolEvent/);
  assert.match(componentSource, /SessionStatusEvent/);
  assert.doesNotMatch(componentSource, /SessionUsageEvent/);
  assert.match(componentSource, /event\.images/);
  assert.match(toolComponentSource, /SessionEventImages/);

  const pageSource = readFileSync(
    new URL('../src/pages/SessionDetailPage.vue', import.meta.url),
    'utf8',
  );
  assert.match(pageSource, /event\.kind !== 'usage'/);
  assert.match(pageSource, /const latestTokenUsage = computed/);
  assert.match(pageSource, /Token 用量/);
  assert.match(pageSource, /latestTokenUsage\.totalTokens/);
});

test('session detail buffers live events while loading the transcript snapshot', () => {
  const composableSource = readFileSync(
    new URL('../src/composables/useSessionDetail.ts', import.meta.url),
    'utf8',
  );
  const sessionsSource = readFileSync(
    new URL('../src/services/sessions.ts', import.meta.url),
    'utf8',
  );
  const pageSource = readFileSync(
    new URL('../src/pages/SessionDetailPage.vue', import.meta.url),
    'utf8',
  );

  assert.match(composableSource, /bufferedLiveEvents = appendLiveEvent/);
  assert.match(composableSource, /eventSnapshotRequests/);
  assert.match(composableSource, /sessionRequests/);
  assert.match(composableSource, /questionRequests/);
  assert.match(composableSource, /mergeSnapshotEvents/);
  assert.match(composableSource, /subscribeSessionStateUpdates\(sessionId/);
  assert.equal(composableSource.includes('subscribeSessionCardChanged'), false);
  assert.equal(composableSource.includes('subscribePendingQuestionBatches'), false);
  assert.match(sessionsSource, /sessionEvents\(sessionId: \$sessionId\) \{\s*ready\s*event/s);
  assert.match(sessionsSource, /data\.sessionEvents\.ready/);
  assert.match(
    sessionsSource,
    /sessionStateUpdates\(sessionId: \$sessionId\) \{\s*ready\s*session/s,
  );
  assert.match(composableSource, /onSubscribed: transcriptReady\.resolve/);
  assert.match(composableSource, /onSubscribed: stateReady\.resolve/);
  assert.equal(
    composableSource.includes('ready: Promise.all([transcriptReady.promise, stateReady.promise])'),
    false,
  );
  assert.match(composableSource, /waitWithTimeout\(registration\.transcriptReady/);
  assert.match(composableSource, /waitWithTimeout\(registration\.stateReady/);
  assert.match(composableSource, /registration\.transcriptReady\.then/);
  assert.match(composableSource, /registration\.stateReady\.then/);
  assert.match(
    pageSource,
    /await startLiveUpdates\(\);\s*if \(!mounted\) return;\s*await Promise\.all/s,
  );
});

test('session detail removes the old pending-question watcher', () => {
  const pageSource = readFileSync(
    new URL('../src/pages/SessionDetailPage.vue', import.meta.url),
    'utf8',
  );

  assert.equal(pageSource.includes('watch(\n  isWaitingForAnswer'), false);
});

test('closed session detail removes the prompt area instead of showing a hint', () => {
  const pageSource = readFileSync(
    new URL('../src/pages/SessionDetailPage.vue', import.meta.url),
    'utf8',
  );

  assert.match(pageSource, /<div v-if="!isClosed" class="detail-composer">/);
  assert.doesNotMatch(pageSource, /detail-closed-banner/);
  assert.doesNotMatch(pageSource, /卡片已关闭，工作树与分支已清理/);
});

test('subscription schema exposes only session-scoped transcript and unified state streams', () => {
  const schemaSource = readFileSync(
    new URL('../../internal/interfaces/graphql/graph/schema.graphqls', import.meta.url),
    'utf8',
  );
  const sessionsSource = readFileSync(
    new URL('../src/services/sessions.ts', import.meta.url),
    'utf8',
  );

  assert.match(schemaSource, /sessionEvents\(sessionId: ID!\): SessionEventStreamItem!/);
  assert.equal(schemaSource.includes('sessionStatusChanged'), false);
  assert.equal(schemaSource.includes('input SessionEventsInput'), false);
  assert.match(sessionsSource, /subscription SessionEvents\(\$sessionId: ID!\)/);
  assert.equal(sessionsSource.includes("codexType === 'process.exit'"), false);
});

test('session detail uses exactly two logical subscriptions', () => {
  const composableSource = readFileSync(
    new URL('../src/composables/useSessionDetail.ts', import.meta.url),
    'utf8',
  );

  const calls = composableSource.match(/= subscribe[A-Z][A-Za-z]+\(/g) ?? [];
  assert.deepEqual(calls.sort(), ['= subscribeSessionEvents(', '= subscribeSessionStateUpdates(']);
});

test('late session state readiness does not reload the Codex transcript', () => {
  const composableSource = readFileSync(
    new URL('../src/composables/useSessionDetail.ts', import.meta.url),
    'utf8',
  );
  const stateLateStart = composableSource.indexOf('registration.stateReady.then');
  const stateLateEnd = composableSource.indexOf('\n      });', stateLateStart);
  const stateLateHandler = composableSource.slice(stateLateStart, stateLateEnd);

  assert.match(stateLateHandler, /loadSessionState\(\)/);
  assert.doesNotMatch(stateLateHandler, /loadSessionDetail\(\)/);
});

test('session detail never drops distinct transcript events by content or timestamp', () => {
  const pageSource = readFileSync(
    new URL('../src/pages/SessionDetailPage.vue', import.meta.url),
    'utf8',
  );

  assert.equal(pageSource.includes('dedupeStreamEntries'), false);
  assert.equal(pageSource.includes('< 1500'), false);
});

test('session route changes remount the detail page and release its operations', () => {
  const layoutSource = readFileSync(
    new URL('../src/layouts/MainLayout.vue', import.meta.url),
    'utf8',
  );

  assert.match(
    layoutSource,
    /<router-view\s+:key="`\$\{\$route\.fullPath\}:\$\{pageRefreshKey\}`"/,
  );
});

test('subscription close before acknowledgement still releases the snapshot gate', () => {
  const transportSource = readFileSync(
    new URL('../src/services/graphqlSubscriptionTransport.js', import.meta.url),
    'utf8',
  );
  const closeHandler = transportSource.slice(
    transportSource.indexOf("socket.addEventListener('close'"),
    transportSource.indexOf(
      'return state',
      transportSource.indexOf("socket.addEventListener('close'"),
    ),
  );

  assert.match(closeHandler, /acknowledged: state\.acknowledged/);
  assert.match(closeHandler, /completedByServer: false/);
  assert.match(transportSource, /completedByServer: true/);
});

test('session detail reopens acknowledged subscriptions completed by the server', () => {
  const composableSource = readFileSync(
    new URL('../src/composables/useSessionDetail.ts', import.meta.url),
    'utf8',
  );

  assert.match(
    composableSource,
    /shouldReconnectAfterClose\(\s*close\.acknowledged,\s*accessKeyValid,\s*close\.completedByServer/s,
  );
  assert.match(
    composableSource,
    /if \(shouldReconnectAfterClose[\s\S]*?scheduleReconnect\(\);[\s\S]*?if \(close\.completedByServer\) return;/,
  );
  assert.match(composableSource, /if \(close\.completedByServer\) return;/);
});

test('subscription refresh does not force a scrolled transcript back to the bottom', () => {
  const pageSource = readFileSync(
    new URL('../src/pages/SessionDetailPage.vue', import.meta.url),
    'utf8',
  );

  assert.doesNotMatch(
    pageSource,
    /watch\(\s*\(\) => loading\.value,[\s\S]*?scrollEventsToBottom\(\)/,
  );
  assert.match(
    pageSource,
    /await Promise\.all\(\[loadSessionDetail\(\), loadPendingQuestions\(\)\]\);\s*if \(!mounted\) return;\s*await scrollEventsToBottom\(\)/,
  );
  assert.match(pageSource, /function isEventStreamAtBottom\(body: HTMLElement\)[\s\S]*?<= 1/);
  assert.match(pageSource, /\{ flush: 'pre' \}/);
  assert.doesNotMatch(pageSource, /< 96/);
  assert.match(pageSource, /let preservingOlderEventScroll = false/);
  assert.match(
    pageSource,
    /if \(loadingOlderEvents\.value \|\| preservingOlderEventScroll\) return/,
  );
  assert.match(
    pageSource,
    /preservingOlderEventScroll = true;[\s\S]*?finally \{\s*preservingOlderEventScroll = false;/,
  );
});

test('exec events render selectable command code separately from terminal output', () => {
  const componentSource = readFileSync(
    new URL('../src/components/SessionToolEvent.vue', import.meta.url),
    'utf8',
  );
  const terminalSource = readFileSync(
    new URL('../src/components/SessionTerminalOutput.vue', import.meta.url),
    'utf8',
  );

  assert.match(componentSource, /<pre[^>]*class="tool-event__command"[^>]*>/);
  assert.match(componentSource, /<code>\{\{ event\.execInput \}\}<\/code>/);
  assert.doesNotMatch(componentSource, /<SessionTerminalOutput :body="event\.execInput/);
  assert.match(componentSource, /<SessionTerminalOutput :body="event\.execOutput"/);
  assert.match(
    componentSource,
    /<SessionTerminalOutput v-else-if="event\.body" :body="event\.body"/,
  );
  assert.match(terminalSource, /renderTerminal,\s*\{ flush: 'post' \}/);
  assert.doesNotMatch(terminalSource, /nextTick\(renderTerminal\)/);
  assert.match(terminalSource, /cursorInactiveStyle: 'none'/);
  assert.match(terminalSource, /@pointerup="blurTerminal"/);
  assert.match(terminalSource, /user-select:\s*text/);
});

test('older event loading crosses pages that add no visible height', () => {
  const pageSource = readFileSync(
    new URL('../src/pages/SessionDetailPage.vue', import.meta.url),
    'utf8',
  );

  assert.match(pageSource, /while \(mounted && body\.scrollHeight <= previousHeight\)/);
  assert.match(pageSource, /const requestedCursor = eventsPageInfo\.value\.nextCursor/);
  assert.match(pageSource, /eventsPageInfo\.value\.nextCursor === requestedCursor/);
});

test('card subscriptions validate pre-ack closes before reconnecting', () => {
  const sessionsPageSource = readFileSync(
    new URL('../src/composables/useSessionsPage.ts', import.meta.url),
    'utf8',
  );
  const overviewSource = readFileSync(
    new URL('../src/pages/IndexPage.vue', import.meta.url),
    'utf8',
  );
  const sessionsServiceSource = readFileSync(
    new URL('../src/services/sessions.ts', import.meta.url),
    'utf8',
  );

  assert.match(sessionsPageSource, /onClose: \(close\) =>[\s\S]*handleSubscriptionClose\(close\)/);
  assert.match(
    sessionsPageSource,
    /shouldReconnectCardStream\(close, \(\) =>\s*verifyGraphQLAccessKey\(getGraphQLAccessKey\(\)\)/,
  );
  assert.match(
    overviewSource,
    /onClose: \(close\) =>[\s\S]*handleOverviewSubscriptionClose\(close\)/,
  );
  assert.match(
    overviewSource,
    /shouldReconnectCardStream\(close, \(\) =>\s*verifyGraphQLAccessKey\(getGraphQLAccessKey\(\)\)/,
  );
  assert.match(
    sessionsPageSource,
    /async function reconnectFromSnapshot\(\)[\s\S]*await loadSessions\(\);[\s\S]*openSubscription\(refreshAfterSubscriptionReady\)/,
  );
  assert.match(
    overviewSource,
    /onSubscribed: onSubscribed \?\? refreshOverviewAfterSubscriptionReady/,
  );
  assert.match(sessionsServiceSource, /sessionCardUpdates[\s\S]*ready[\s\S]*card \{/);
  assert.match(sessionsServiceSource, /handlers\.onSubscribed\?\.\(\)/);
});

test('session list loads freeze their scope and reject stale responses', () => {
  const sessionsPageSource = readFileSync(
    new URL('../src/composables/useSessionsPage.ts', import.meta.url),
    'utf8',
  );

  assert.match(sessionsPageSource, /const loadRequests = createLatestRequestTracker\(\)/);
  assert.match(sessionsPageSource, /const requestGeneration = loadRequests\.next\(\)/);
  assert.match(sessionsPageSource, /const requestInput = \{ \.\.\.input\.value \}/);
  assert.match(sessionsPageSource, /listSessions\(\{\s*\.\.\.requestInput,/s);
  assert.match(sessionsPageSource, /if \(!loadRequests\.isCurrent\(requestGeneration\)\) return;/);
  assert.match(
    sessionsPageSource,
    /finally \{\s*if \(loadRequests\.isCurrent\(requestGeneration\)\) \{\s*loading\.value = false;/s,
  );
});

test('session state remains independent from transcript snapshot failures', () => {
  const composableSource = readFileSync(
    new URL('../src/composables/useSessionDetail.ts', import.meta.url),
    'utf8',
  );
  const commitHistorySource = readFileSync(
    new URL('../src/pages/CommitHistoryPage.vue', import.meta.url),
    'utf8',
  );
  const sessionsServiceSource = readFileSync(
    new URL('../src/services/sessions.ts', import.meta.url),
    'utf8',
  );

  assert.doesNotMatch(composableSource, /getSessionDetail/);
  assert.match(
    composableSource,
    /Promise\.allSettled\(\[\s*getSession\(sessionId\),\s*getSessionEventPage\(sessionId, '', eventPageSize\),?\s*\]\)/,
  );
  assert.match(composableSource, /sessionResult\.status === 'fulfilled'/);
  assert.match(composableSource, /eventResult\.status === 'fulfilled'/);
  assert.match(commitHistorySource, /getSession\(sessionId\)/);
  assert.doesNotMatch(commitHistorySource, /getSessionDetail/);
  assert.doesNotMatch(sessionsServiceSource, /export async function getSessionDetail/);
});
