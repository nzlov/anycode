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
  const sessionsSource = readFileSync(
    new URL('../src/services/sessions.ts', import.meta.url),
    'utf8',
  );
  const composableSource = readFileSync(
    new URL('../src/composables/useSessionDetail.ts', import.meta.url),
    'utf8',
  );

  assert.match(timelineSource, /\.\.\. on TranscriptMessageContent/);
  assert.match(timelineSource, /\.\.\. on TranscriptCommandContent/);
  assert.match(timelineSource, /kind/);
  assert.match(
    timelineSource,
    /commands \{ command workdir hasOutput output exitCode durationMs \}/,
  );
  assert.doesNotMatch(timelineSource, /TranscriptCommandContent \{ command output/);
  assert.match(timelineSource, /\.\.\. on TranscriptToolContent/);
  assert.match(
    sessionsSource,
    /const sessionDetailFields = `[\s\S]*usage \{ \$\{transcriptUsageFields\} \}/,
  );
  assert.match(composableSource, /tokenUsage\.value = sessionResult\.value\.usage \?\? null/);
  assert.doesNotMatch(composableSource, /tokenUsage\.value = eventResult\.value\.usage/);
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
  assert.match(pageSource, /latestTokenUsage\.currentInputTokens/);
  assert.match(pageSource, /latestTokenUsage\.inputTokens/);
  assert.match(pageSource, /contextUsagePercent/);
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

test('session detail loads the first transcript page before starting subscriptions', () => {
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

  assert.match(composableSource, /events\.value = eventResult\.value\.items/);
  assert.match(composableSource, /detailRequests/);
  assert.match(composableSource, /sessionRequests/);
  assert.match(composableSource, /questionRequests/);
  assert.match(composableSource, /subscribeSessionEvents\(sessionId/);
  assert.match(composableSource, /useSessionUpdates\(\{/);
  assert.equal(composableSource.includes('subscribeSessionCardChanged'), false);
  assert.equal(composableSource.includes('subscribePendingQuestionBatches'), false);
  assert.doesNotMatch(
    composableSource,
    /bufferedLiveEvents|mergeSnapshotEvents|onSubscribed|registration\.ready/,
  );
  assert.doesNotMatch(timelineSource, /subscription SessionTranscript/);
  assert.match(
    sessionsSource,
    /sessionEvents\(sessionId: \$sessionId\) \{\s*\$\{transcriptEventFields\}\s*\}/s,
  );
  assert.doesNotMatch(sessionsSource, /sessionEvents[\s\S]*ready/);
  assert.match(
    pageSource,
    /await Promise\.all\(\[loadSessionDetail\(\), loadPendingQuestions\(\)\]\);\s*if \(!mounted\) return;\s*startLiveUpdates\(\)/s,
  );
});

test('session detail applies todo updates to the existing session state', () => {
  const sessionsSource = readFileSync(
    new URL('../src/services/sessions.ts', import.meta.url),
    'utf8',
  );
  const detailFieldsStart = sessionsSource.indexOf('const sessionDetailFields');
  const detailFieldsEnd = sessionsSource.indexOf('const sessionFields', detailFieldsStart);
  const detailFields = sessionsSource.slice(detailFieldsStart, detailFieldsEnd);
  const normalizerStart = sessionsSource.indexOf('function normalizeSessionDetail');
  const normalizerEnd = sessionsSource.indexOf('function normalizePromptAppend', normalizerStart);
  const normalizer = sessionsSource.slice(normalizerStart, normalizerEnd);

  assert.match(detailFields, /todoList \{\s*completed\s*total\s*items \{\s*text\s*completed/s);
  assert.match(normalizer, /todoList: normalizeTodoList\(session\.todoList\)/);
  assert.doesNotMatch(normalizer, /todoList: null,/);
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

  assert.match(pageSource, /<div\s+v-if="!isClosed"\s+class="detail-composer"/);
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
  assert.match(pageSource, /<div v-else-if="isWaitingForApproval" class="detail-approval-review">/);
  assert.match(pageSource, /<WorkflowResultReview/);
  assert.match(pageSource, /:phase="session\?\.pendingApproval\?\.phase \?\? null"/);
  assert.match(pageSource, /:result="session\?\.pendingApproval\?\.result \?\? null"/);
  assert.match(pageSource, /:key="approvalPanelKey"/);
  assert.match(
    pageSource,
    /`\$\{approval\.sessionId\}:\$\{approval\.nodeId\}:\$\{approval\.nodeRunId \?\? ''\}`/,
  );
  assert.match(pageSource, /<CodexPromptComposer\s+v-else/);
  assert.match(pageSource, /isPendingApprovalReviewable\(session\?\.pendingApproval\)/);
  assert.match(composableSource, /submitWorkflowApproval as submitWorkflowApprovalRequest/);
  assert.match(
    composableSource,
    /async function submitApproval\(approved: boolean, comment: string\)/,
  );
  assert.match(composableSource, /await loadSessionState\(\)/);
  assert.match(composableSource, /if \(!isPendingApprovalReviewable\(approval\)\)/);
  assert.doesNotMatch(approvalPanelSource, /SessionEventMessage|DiffViewer|模型输出|Diff/);
});

test('subscription schema separates transcript events from global session updates', () => {
  const schemaSource = readFileSync(
    new URL('../../internal/interfaces/graphql/graph/schema.graphqls', import.meta.url),
    'utf8',
  );
  const timelineSource = readFileSync(
    new URL('../src/services/sessionTimeline.ts', import.meta.url),
    'utf8',
  );

  assert.match(schemaSource, /sessionEvents\(sessionId: ID!\): TranscriptEvent!/);
  assert.match(schemaSource, /sessionUpdates: SessionUpdateEvent!/);
  assert.doesNotMatch(schemaSource, /SessionEventStreamItem|ready: Boolean/);
  assert.match(schemaSource, /input ListTranscriptEventsInput/);
  assert.match(schemaSource, /type TranscriptEventGroup/);
  assert.match(schemaSource, /members: \[TranscriptEvent!\]!/);
  assert.match(timelineSource, /group \{/);
  assert.doesNotMatch(timelineSource, /subscription SessionTranscript/);
  assert.equal(timelineSource.includes("codexType === 'process.exit'"), false);
});

test('session detail uses one transcript subscription and one global update subscription', () => {
  const composableSource = readFileSync(
    new URL('../src/composables/useSessionDetail.ts', import.meta.url),
    'utf8',
  );

  assert.equal((composableSource.match(/subscribeSessionEvents\(sessionId/g) ?? []).length, 1);
  assert.equal((composableSource.match(/useSessionUpdates\(\{/g) ?? []).length, 1);
});

test('session subscriptions do not wait for readiness or reload on reconnect', () => {
  const composableSource = readFileSync(
    new URL('../src/composables/useSessionDetail.ts', import.meta.url),
    'utf8',
  );
  assert.doesNotMatch(
    composableSource,
    /ready|waitWithTimeout|refreshAfterReconnect|reconnectFromSnapshot/,
  );
  assert.match(composableSource, /reconnectTimer = setTimeout\([\s\S]*openSessionEvents\(\)/);
});

test('session detail never drops distinct transcript events by content or timestamp', () => {
  const pageSource = readFileSync(
    new URL('../src/pages/SessionDetailPage.vue', import.meta.url),
    'utf8',
  );

  assert.equal(pageSource.includes('dedupeStreamEntries'), false);
  assert.equal(pageSource.includes('< 1500'), false);
});

test('session creation preserves the overview page while route changes still remount it', () => {
  const layoutSource = readFileSync(
    new URL('../src/layouts/MainLayout.vue', import.meta.url),
    'utf8',
  );
  const newSessionSource = readFileSync(
    new URL('../src/components/NewSessionDialog.vue', import.meta.url),
    'utf8',
  );

  assert.match(layoutSource, /<router-view\s+:key="\$route\.fullPath"/);
  assert.doesNotMatch(layoutSource, /pageRefreshKey|handleSessionCreated|@create=/);
  assert.doesNotMatch(newSessionSource, /emit\('create'\)/);
});

test('subscription close reports acknowledgement and completion state', () => {
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
  assert.doesNotMatch(transportSource, /snapshot/);
});

test('session detail reopens acknowledged subscriptions completed by the server', () => {
  const composableSource = readFileSync(
    new URL('../src/composables/useSessionDetail.ts', import.meta.url),
    'utf8',
  );

  assert.match(
    composableSource,
    /shouldReconnectSubscription\(close, \(\) =>\s*verifyGraphQLAccessKey\(getGraphQLAccessKey\(\)\)/,
  );
  assert.match(composableSource, /if \(reconnect\) \{\s*scheduleReconnect\(\)/);
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
    /await Promise\.all\(\[loadSessionDetail\(\), loadPendingQuestions\(\)\]\);\s*if \(!mounted\) return;\s*startLiveUpdates\(\);\s*await scrollEventsToBottom\(\)/,
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
    /preservingOlderEventScroll = true;[\s\S]*?finally \{\s*previousEventScrollTop = body\.scrollTop;\s*preservingOlderEventScroll = false;/,
  );
});

test('exec and shell events share a type-only header and group command input and output', () => {
  const componentSource = readFileSync(
    new URL('../src/components/SessionCommandEvent.vue', import.meta.url),
    'utf8',
  );
  const ansiSource = readFileSync(
    new URL('../src/components/StaticAnsiOutput.vue', import.meta.url),
    'utf8',
  );

  assert.match(componentSource, /content\.value\.kind === 'exec' \? 'Exec' : 'Shell'/);
  assert.match(componentSource, /class="command-event__title">\{\{ title \}\}<\/span>/);
  assert.doesNotMatch(componentSource, /firstCommand\?\.command/);
  assert.match(componentSource, /class="command-event__invocation"/);
  assert.match(componentSource, /class="command-event__input"/);
  assert.match(componentSource, /class="command-event__label">输入<\/div>/);
  assert.match(componentSource, /v-for="\(command, index\) in content\.commands"/);
  assert.match(componentSource, /命令 \{\{ index \+ 1 \}\}/);
  assert.match(componentSource, /<code>\{\{ command\.command \}\}<\/code>/);
  assert.match(componentSource, /v-if="command\.hasOutput"/);
  assert.match(componentSource, /<StaticAnsiOutput :text="command\.output" appearance="surface"/);
  assert.match(componentSource, /class="command-event__workdir">\{\{ command\.workdir \}\}/);
  assert.doesNotMatch(componentSource, /content\.output|unassignedOutput/);
  assert.match(componentSource, /:disabled="!canExpand"/);
  assert.match(componentSource, /content\.value\.commands\.length/);
  assert.match(componentSource, /class="command-event__title"/);
  assert.doesNotMatch(componentSource, /\.command-event__header span/);
  assert.match(componentSource, /\.command-event__header:not\(:disabled\):hover/);
  assert.match(componentSource, /\.command-event__input,[\s\S]*?min-width:\s*0/);
  assert.match(componentSource, /\.command-event__command\s*\{[^}]*max-width:\s*100%/s);
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
  const themeSource = readFileSync(new URL('../src/css/theme.scss', import.meta.url), 'utf8');

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

test('live usage updates token totals directly from the global session stream', () => {
  const source = readFileSync(
    new URL('../src/composables/useSessionDetail.ts', import.meta.url),
    'utf8',
  );

  assert.match(source, /if \(update\.usage\) tokenUsage\.value = update\.usage/);
  assert.doesNotMatch(source, /bufferedLiveUsage|bufferingLiveEvents/);
});

test('overview cards load persisted usage and apply live usage updates', () => {
  const overviewSource = readFileSync(
    new URL('../src/pages/IndexPage.vue', import.meta.url),
    'utf8',
  );
  const sessionsSource = readFileSync(
    new URL('../src/services/sessions.ts', import.meta.url),
    'utf8',
  );
  const usageDisplaySource = readFileSync(
    new URL('../src/components/TokenUsageDisplay.vue', import.meta.url),
    'utf8',
  );

  assert.match(
    sessionsSource,
    /const sessionCardFields = `[\s\S]*usage \{ \$\{transcriptUsageFields\} \}/,
  );
  assert.match(
    overviewSource,
    /if \(update\.usage\) \{\s*next = \{ \.\.\.next, usage: update\.usage \};/s,
  );
  assert.match(overviewSource, /<TokenUsageDisplay :usage="card\.usage"/);
  assert.match(usageDisplaySource, /formatTokenCount\(usage\.totalTokens\)/);
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

test('older event loading ignores follow-up layout scroll events', () => {
  const pageSource = readFileSync(
    new URL('../src/pages/SessionDetailPage.vue', import.meta.url),
    'utf8',
  );

  assert.match(pageSource, /let previousEventScrollTop = Number\.POSITIVE_INFINITY/);
  assert.match(pageSource, /const scrollingUp = currentScrollTop < previousEventScrollTop/);
  assert.match(pageSource, /if \(!scrollingUp \|\| currentScrollTop > 64/);
  assert.match(
    pageSource,
    /finally \{\s*previousEventScrollTop = body\.scrollTop;\s*preservingOlderEventScroll = false;/,
  );
});

test('the list has no subscription and pages share the global update lifecycle', () => {
  const sessionsPageSource = readFileSync(
    new URL('../src/composables/useSessionsPage.ts', import.meta.url),
    'utf8',
  );
  const overviewSource = readFileSync(
    new URL('../src/pages/IndexPage.vue', import.meta.url),
    'utf8',
  );
  const updateComposableSource = readFileSync(
    new URL('../src/composables/useSessionUpdates.ts', import.meta.url),
    'utf8',
  );
  const sessionsServiceSource = readFileSync(
    new URL('../src/services/sessions.ts', import.meta.url),
    'utf8',
  );

  assert.doesNotMatch(sessionsPageSource, /subscribe|startLiveUpdates|stopLiveUpdates/);
  assert.match(overviewSource, /useSessionUpdates\(\{\s*onData: handleSessionUpdate/s);
  assert.equal((overviewSource.match(/useSessionUpdates\(\{/g) ?? []).length, 1);
  assert.match(
    updateComposableSource,
    /shouldReconnectSubscription\(close, \(\) =>\s*verifyGraphQLAccessKey\(getGraphQLAccessKey\(\)\)/,
  );
  assert.match(sessionsServiceSource, /sessionUpdates \{[\s\S]*eventType[\s\S]*sessionId/);
  assert.doesNotMatch(sessionsServiceSource, /sessionCardUpdates|onSubscribed|ready/);
});

test('headless lifecycle audit observes real GraphQL WebSocket operations across routes', () => {
  const headlessSource = readFileSync(
    new URL('../../scripts/headless-e2e.mjs', import.meta.url),
    'utf8',
  );

  assert.match(headlessSource, /--subscription-lifecycle-only/);
  assert.match(headlessSource, /Network\.webSocketFrameSent/);
  assert.match(headlessSource, /assertSubscriptionOperationLifecycle/);
  assert.match(headlessSource, /initial sessions list', \[\]/);
  assert.match(headlessSource, /'overview', \['sessionUpdates'\]/);
  assert.match(headlessSource, /'session detail', \[\s*'sessionEvents',\s*'sessionUpdates'/s);
  assert.match(headlessSource, /'list after detail', \[\]/);
  assert.match(headlessSource, /subscription-lifecycle\.json/);
});

test('overview invalidates late card requests and waiting dialogs across its subscription lifecycle', () => {
  const overviewSource = readFileSync(
    new URL('../src/pages/IndexPage.vue', import.meta.url),
    'utf8',
  );

  assert.match(overviewSource, /onMounted\(\(\) => \{\s*overviewMounted = true;/);
  assert.match(
    overviewSource,
    /onUnmounted\(\(\) => \{\s*overviewMounted = false;\s*cardRefreshRequests\.clear\(\);/,
  );
  assert.match(
    overviewSource,
    /await loadOverviewSessions\(\);\s*if \(!overviewMounted\) return;\s*startOverviewLiveUpdates\(\);/,
  );
  assert.match(overviewSource, /cardRefreshRequests\.invalidate\(update\.sessionId\)/);
  assert.match(
    overviewSource,
    /!overviewMounted \|\| !cardRefreshRequests\.isCurrent\(sessionId, generation\)/,
  );
  assert.doesNotMatch(overviewSource, /activeCardRefreshes|repeatedCardRefreshes/);
  assert.match(
    overviewSource,
    /activeQuestionSessionId\.value === update\.sessionId && status && status !== 'waiting_user'/,
  );
  assert.match(
    overviewSource,
    /approvalSessionId\.value === update\.sessionId && status && status !== 'waiting_approval'/,
  );
  assert.match(
    overviewSource,
    /detail\.status !== 'waiting_approval' \|\| !detail\.pendingApproval/,
  );
  assert.match(overviewSource, /card\?\.status !== 'waiting_user' \|\| batches\.length === 0/);
});

test('known session updates patch event payloads without automatic card or detail queries', () => {
  const overviewSource = readFileSync(
    new URL('../src/pages/IndexPage.vue', import.meta.url),
    'utf8',
  );
  const detailSource = readFileSync(
    new URL('../src/composables/useSessionDetail.ts', import.meta.url),
    'utf8',
  );
  const sessionsSource = readFileSync(
    new URL('../src/services/sessions.ts', import.meta.url),
    'utf8',
  );
  const overviewHandler = overviewSource.slice(
    overviewSource.indexOf('function handleSessionUpdate'),
    overviewSource.indexOf('function refreshOverviewCard'),
  );
  const knownOverviewUpdate = overviewHandler.slice(overviewHandler.indexOf('const current'));
  const detailHandler = detailSource.slice(
    detailSource.indexOf('function handleSessionUpdate'),
    detailSource.indexOf('\n  return {', detailSource.indexOf('function handleSessionUpdate')),
  );

  assert.equal(
    (overviewHandler.match(/refreshOverviewCard\(update\.sessionId\)/g) ?? []).length,
    1,
  );
  assert.match(
    overviewHandler,
    /if \(index < 0\) \{\s*if \(update\.status\) void refreshOverviewCard\(update\.sessionId\);/,
  );
  assert.doesNotMatch(
    knownOverviewUpdate,
    /refreshOverviewCard|getSessionCard|loadOverviewSessions/,
  );
  assert.match(knownOverviewUpdate, /update\.priority/);
  assert.match(knownOverviewUpdate, /update\.availableActions !== undefined/);
  assert.match(knownOverviewUpdate, /update\.updatedAt && update\.updatedTime/);
  assert.match(detailHandler, /update\.config/);
  assert.match(detailHandler, /update\.worktreeCleanup/);
  assert.equal((detailHandler.match(/loadSessionState\(\)/g) ?? []).length, 1);
  assert.match(
    detailHandler,
    /status === 'waiting_approval'[\s\S]*void loadSessionState\(\)\.finally/,
  );
  assert.match(sessionsSource, /priority\s+config \{/);
  assert.match(sessionsSource, /worktreeCleanup \{/);
  assert.match(sessionsSource, /availableActions\s+updatedAt/);
});

test('overview answer submission closes the dialog without refetching questions or the card', () => {
  const overviewSource = readFileSync(
    new URL('../src/pages/IndexPage.vue', import.meta.url),
    'utf8',
  );
  const submitBlock = overviewSource.slice(
    overviewSource.indexOf('async function submitAnswers'),
    overviewSource.indexOf('async function openApprovalDialog'),
  );

  assert.match(submitBlock, /await submitQuestionBatch\(batchId, answers\)/);
  assert.match(submitBlock, /answerDialog\.value = false/);
  assert.doesNotMatch(submitBlock, /getPendingQuestionBatches|refreshOverviewCard|getSessionCard/);
});

test('detail approval loads cannot clear a newer waiting-state request', () => {
  const composableSource = readFileSync(
    new URL('../src/composables/useSessionDetail.ts', import.meta.url),
    'utf8',
  );

  assert.match(composableSource, /const generation = \+\+approvalLoadGeneration/);
  assert.match(
    composableSource,
    /if \(generation === approvalLoadGeneration\) approvalLoading\.value = false/,
  );
  assert.match(composableSource, /else if \(status\) \{\s*approvalLoadGeneration \+= 1;/);
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

test('session state remains independent from transcript first-page failures', () => {
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
