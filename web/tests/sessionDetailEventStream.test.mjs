import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

import Anser from 'anser';

import { stripUnsupportedAnsiControls } from '../src/services/sessionTimelinePresentation.ts';

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
  const timelineSource = readFileSync(
    new URL('../src/services/sessionTimeline.ts', import.meta.url),
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

  assert.match(timelineSource, /\.\.\. on TranscriptMessageContent/);
  assert.match(timelineSource, /\.\.\. on TranscriptCommandContent/);
  assert.match(timelineSource, /commands \{ command workdir \}/);
  assert.doesNotMatch(timelineSource, /TranscriptCommandContent \{ command output/);
  assert.match(timelineSource, /\.\.\. on TranscriptToolContent/);
  assert.match(timelineSource, /usage \{ \$\{transcriptUsageFields\} \}/);
  assert.match(componentSource, /SessionToolEvent/);
  assert.match(componentSource, /SessionStatusEvent/);
  assert.doesNotMatch(componentSource, /SessionUsageEvent/);
  assert.match(toolComponentSource, /content\.images/);

  const pageSource = readFileSync(
    new URL('../src/pages/SessionDetailPage.vue', import.meta.url),
    'utf8',
  );
  assert.match(pageSource, /const latestTokenUsage = computed/);
  assert.match(pageSource, /Token 用量/);
  assert.match(pageSource, /latestTokenUsage\.totalTokens/);
});

test('session text messages fold runtime context and AnyCode guidance', () => {
  const componentSource = readFileSync(
    new URL('../src/components/SessionTextMessage.vue', import.meta.url),
    'utf8',
  );
  const presentationSource = readFileSync(
    new URL('../src/services/sessionTimelinePresentation.ts', import.meta.url),
    'utf8',
  );
  const pageSource = readFileSync(
    new URL('../src/pages/SessionDetailPage.vue', import.meta.url),
    'utf8',
  );

  assert.match(componentSource, /sessionTextPresentation/);
  assert.match(componentSource, /knownUserPrompts/);
  assert.match(componentSource, /workflowPrompt/);
  assert.match(componentSource, /presentation\.foldedLabel/);
  assert.match(componentSource, /:aria-expanded="expanded"/);
  assert.match(presentationSource, /# AGENTS\.md instructions for/);
  assert.match(presentationSource, /<environment_context>/);
  assert.match(presentationSource, /AnyCode 附加说明/);
  assert.match(pageSource, /const knownUserPrompts = computed/);
  assert.match(pageSource, /:known-user-prompts="knownUserPrompts"/);
  assert.match(pageSource, /:workflow-prompt="session\?\.mode === 'workflow'"/);
});

test('session detail buffers live events while loading the transcript snapshot', () => {
  const composableSource = readFileSync(
    new URL('../src/composables/useSessionDetail.ts', import.meta.url),
    'utf8',
  );
  const timelineSource = readFileSync(
    new URL('../src/services/sessionTimeline.ts', import.meta.url),
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
  assert.match(timelineSource, /sessionTranscript\(sessionId: \$sessionId\) \{\s*ready\s*event/s);
  assert.match(timelineSource, /data\.sessionTranscript\.ready/);
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

test('session detail replaces the prompt composer with the shared inline approval panel', () => {
  const pageSource = readFileSync(
    new URL('../src/pages/SessionDetailPage.vue', import.meta.url),
    'utf8',
  );
  const composableSource = readFileSync(
    new URL('../src/composables/useSessionDetail.ts', import.meta.url),
    'utf8',
  );
  const approvalPanelSource = readFileSync(
    new URL('../src/components/WorkflowApprovalPanel.vue', import.meta.url),
    'utf8',
  );

  assert.match(pageSource, /<q-card v-if="isWaitingForAnswer"/);
  assert.match(pageSource, /<WorkflowApprovalPanel\s+v-else-if="isWaitingForApproval"/);
  assert.match(pageSource, /:key="approvalPanelKey"/);
  assert.match(
    pageSource,
    /`\$\{approval\.workflowRunId\}:\$\{approval\.nodeId\}:\$\{approval\.nodeRunId\}`/,
  );
  assert.match(pageSource, /<CodexPromptComposer\s+v-else/);
  assert.match(pageSource, /Boolean\(session\?\.pendingApproval\)/);
  assert.match(composableSource, /submitWorkflowApproval as submitWorkflowApprovalRequest/);
  assert.match(
    composableSource,
    /async function submitApproval\(approved: boolean, comment: string\)/,
  );
  assert.match(composableSource, /await loadSessionState\(\)/);
  assert.doesNotMatch(approvalPanelSource, /SessionEventMessage|DiffViewer|模型输出|Diff/);
});

test('subscription schema exposes only session-scoped transcript and unified state streams', () => {
  const schemaSource = readFileSync(
    new URL('../../internal/interfaces/graphql/graph/schema.graphqls', import.meta.url),
    'utf8',
  );
  const timelineSource = readFileSync(
    new URL('../src/services/sessionTimeline.ts', import.meta.url),
    'utf8',
  );

  assert.match(schemaSource, /sessionTranscript\(sessionId: ID!\): TranscriptStreamItem!/);
  assert.equal(schemaSource.includes('sessionStatusChanged'), false);
  assert.equal(schemaSource.includes('input SessionTranscriptInput'), false);
  assert.match(schemaSource, /input ListTranscriptEventsInput/);
  assert.match(timelineSource, /subscription SessionTranscript\(\$sessionId: ID!\)/);
  assert.equal(timelineSource.includes("codexType === 'process.exit'"), false);
});

test('session detail uses exactly two logical subscriptions', () => {
  const composableSource = readFileSync(
    new URL('../src/composables/useSessionDetail.ts', import.meta.url),
    'utf8',
  );

  const calls = composableSource.match(/= subscribe[A-Z][A-Za-z]+\(/g) ?? [];
  assert.deepEqual(calls.sort(), [
    '= subscribeSessionStateUpdates(',
    '= subscribeSessionTranscript(',
  ]);
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

test('exec events keep the first command in the header and expose additional commands', () => {
  const componentSource = readFileSync(
    new URL('../src/components/SessionCommandEvent.vue', import.meta.url),
    'utf8',
  );
  const ansiSource = readFileSync(
    new URL('../src/components/StaticAnsiOutput.vue', import.meta.url),
    'utf8',
  );

  assert.match(componentSource, /content\.value\.commands\.slice\(1\)/);
  assert.match(componentSource, /v-for="\(command, index\) in additionalCommands"/);
  assert.match(componentSource, /命令 \{\{ index \+ 2 \}\}/);
  assert.match(componentSource, /<code>\{\{ command\.command \}\}<\/code>/);
  assert.match(componentSource, /`\+\$\{additionalCommandCount\} 条`/);
  assert.match(componentSource, /class="command-event__workdir">\{\{ command\.workdir \}\}/);
  assert.match(componentSource, /<StaticAnsiOutput :text="content\.output" appearance="surface"/);
  assert.match(componentSource, /:disabled="!canExpand"/);
  assert.match(componentSource, /firstCommand\.value\?\.workdir/);
  assert.match(componentSource, /class="command-event__title"/);
  assert.doesNotMatch(componentSource, /\.command-event__header span/);
  assert.match(componentSource, /\.command-event__header:not\(:disabled\):hover/);
  assert.doesNotMatch(componentSource, /SessionTerminalOutput/);
  assert.match(ansiSource, /Anser\.ansiToJson/);
  assert.match(ansiSource, /user-select:\s*text/);
  assert.doesNotMatch(ansiSource, /contextmenu|cursorInactiveStyle|@pointerup/);
});

test('static ANSI rendering uses theme colors and preserves extended RGB colors', () => {
  const ansiSource = readFileSync(
    new URL('../src/components/StaticAnsiOutput.vue', import.meta.url),
    'utf8',
  );
  const themeSource = readFileSync(new URL('../src/css/app.scss', import.meta.url), 'utf8');

  assert.match(ansiSource, /value\.replaceAll\(' ', ''\)/);
  assert.match(ansiSource, /themedColors\[normalized\] \?\? `rgb\(\$\{normalized\}\)`/);
  assert.match(ansiSource, /var\(--ac-ansi-bright-cyan\)/);
  assert.match(ansiSource, /appearance\?: 'terminal' \| 'surface'/);
  assert.match(
    ansiSource,
    /\.static-ansi-output--surface\s*{[^}]*background:\s*var\(--ac-surface\);[^}]*color:\s*var\(--ac-text\);/s,
  );
  assert.match(ansiSource, /segment\.decorations/);
  assert.match(ansiSource, /decorations\.has\('bold'\)/);
  const styled = Anser.ansiToJson('\x1b[1;3;4mstyled').find(
    (segment) => segment.content === 'styled',
  );
  assert.deepEqual(styled?.decorations, ['bold', 'italic', 'underline']);
  assert.match(themeSource, /:root[\s\S]*--ac-terminal-bg:[\s\S]*--ac-ansi-bright-white:/);
  assert.match(themeSource, /:root[\s\S]*--ac-terminal-bg: #111827/);
  assert.match(themeSource, /\.body--dark[\s\S]*--ac-terminal-bg:[\s\S]*--ac-ansi-bright-white:/);
});

test('static ANSI rendering removes unsupported OSC controls without losing visible text', () => {
  const hyperlink = '\x1b]8;;https://example.com\x07link\x1b]8;;\x07';
  const title = '\x1b]0;build output\x1b\\ready';
  const unterminated = 'before\x1b]0;partial title';

  assert.equal(stripUnsupportedAnsiControls(hyperlink), 'link');
  assert.equal(stripUnsupportedAnsiControls(title), 'ready');
  assert.equal(stripUnsupportedAnsiControls(unterminated), 'before');
  assert.deepEqual(
    Anser.ansiToJson(stripUnsupportedAnsiControls(hyperlink)).map((segment) => segment.content),
    ['link'],
  );
});

test('assistant markdown is parsed and sanitized at the rendering boundary', () => {
  const markdownSource = readFileSync(
    new URL('../src/components/MarkdownContent.vue', import.meta.url),
    'utf8',
  );

  assert.match(markdownSource, /marked\.parse/);
  assert.match(markdownSource, /DOMPurify\.sanitize/);
  assert.match(markdownSource, /ALLOWED_TAGS:/);
  assert.match(markdownSource, /ALLOWED_ATTR: \['href', 'title', 'src', 'alt'\]/);
  assert.match(markdownSource, /:deep\(img\)[\s\S]*max-width: 100%;[\s\S]*height: auto;/);
  assert.match(markdownSource, /:deep\(table\)[\s\S]*max-width: 100%;[\s\S]*overflow-x: auto;/);
  const sanitizeConfig = markdownSource.slice(
    markdownSource.indexOf('ALLOWED_TAGS:'),
    markdownSource.indexOf('}),', markdownSource.indexOf('ALLOWED_TAGS:')),
  );
  assert.doesNotMatch(sanitizeConfig, /['"](?:class|id|style)['"]/);
  assert.doesNotMatch(markdownSource, /replace\([^\n]+markdown|renderMarkdown/);
});

test('terminal phases and status details remain visible', () => {
  const commandSource = readFileSync(
    new URL('../src/components/SessionCommandEvent.vue', import.meta.url),
    'utf8',
  );
  const toolSource = readFileSync(
    new URL('../src/components/SessionToolEvent.vue', import.meta.url),
    'utf8',
  );
  const statusSource = readFileSync(
    new URL('../src/components/SessionStatusEvent.vue', import.meta.url),
    'utf8',
  );
  const presentationSource = readFileSync(
    new URL('../src/services/sessionTimelinePresentation.ts', import.meta.url),
    'utf8',
  );

  assert.match(commandSource, /timelinePhaseIcon\(event\.phase\)/);
  assert.match(toolSource, /timelinePhaseLabel\(event\.phase\)/);
  assert.match(presentationSource, /failed: \{ icon: 'error_outline', color: 'negative'/);
  assert.match(presentationSource, /cancelled: \{ icon: 'cancel', color: 'grey-7'/);
  assert.match(statusSource, /Object\.keys\(content\.value\.details\)/);
  assert.match(statusSource, /<StructuredContent v-if="expanded" :content="detailsContent"/);
  assert.match(statusSource, /status-event--error/);
});

test('live usage is buffered while a transcript snapshot is loading', () => {
  const source = readFileSync(
    new URL('../src/composables/useSessionDetail.ts', import.meta.url),
    'utf8',
  );

  assert.match(source, /let bufferedLiveUsage: TranscriptTokenUsage \| null = null/);
  assert.match(source, /tokenUsage\.value = bufferedLiveUsage \?\? eventResult\.value\.usage/);
  assert.match(source, /if \(bufferingLiveEvents\) \{\s*bufferedLiveUsage = usage;/s);
});

test('older timeline pages restore a stable visible event anchor', () => {
  const source = readFileSync(
    new URL('../src/pages/SessionDetailPage.vue', import.meta.url),
    'utf8',
  );

  assert.match(source, /:data-timeline-id="event\.id"/);
  assert.match(source, /const anchor = captureEventScrollAnchor\(body\)/);
  assert.match(source, /restoreEventScrollAnchor\(body, anchor\)/);
  assert.match(source, /candidate\.dataset\.timelineId === anchor\.id/);
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
    /Promise\.allSettled\(\[\s*getSession\(sessionId\),\s*getSessionTranscriptPage\(sessionId, '', eventPageSize\),?\s*\]\)/,
  );
  assert.match(composableSource, /sessionResult\.status === 'fulfilled'/);
  assert.match(composableSource, /eventResult\.status === 'fulfilled'/);
  assert.match(commitHistorySource, /getSession\(sessionId\)/);
  assert.doesNotMatch(commitHistorySource, /getSessionDetail/);
  assert.doesNotMatch(sessionsServiceSource, /export async function getSessionDetail/);
});
