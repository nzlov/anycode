import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const root = new URL('../', import.meta.url);

test('terminal mode uses the overview toolbar shortcut instead of the new session dialog', async () => {
  const [dialog, overview, sessions] = await Promise.all([
    read('src/components/NewSessionDialog.vue'),
    read('src/pages/IndexPage.vue'),
    read('src/services/sessions.ts'),
  ]);
  assert.match(sessions, /SessionMode = 'workflow' \| 'chat' \| 'terminal'/);
  assert.doesNotMatch(dialog, /createSession\('terminal'\)/);
  assert.match(overview, /aria-label="新建 Terminal"/);
  assert.match(overview, /requirement: 'Terminal',[\s\S]*mode: 'terminal'/);
  assert.match(overview, /await refreshOverviewCard\(session\.id\)/);
});

test('terminal socket authenticates in-band and reconnects without putting credentials in the URL', async () => {
  const source = await read('src/services/terminalSocket.ts');
  assert.match(source, /type: 'connection_init'/);
  assert.match(source, /authorization: bearerAuthorization\(\)/);
  assert.doesNotMatch(source, /searchParams|accessKey=/);
  assert.match(source, /binaryType = 'arraybuffer'/);
  assert.match(source, /socket\.send\(encoder\.encode\(data\)\)/);
  assert.match(source, /type: 'resize'/);
  assert.match(source, /Math\.min\(500 \* 2 \*\*/);
});

test('terminal view covers resize, output acknowledgement, native selection, touch scrolling, and keys', async () => {
  const [source, sessionView] = await Promise.all([
    read('src/components/TerminalView.vue'),
    read('src/components/TerminalSessionView.vue'),
  ]);
  assert.match(source, /new ResizeObserver\(fitTerminal\)/);
  assert.match(source, /if \(props\.resizePaused\) \{\s*resizePending = true;\s*return;/);
  assert.match(source, /if \(!paused && resizePending\) void nextTick\(fitTerminal\)/);
  assert.match(source, /terminal\.onData/);
  assert.match(source, /interactive\?: boolean/);
  assert.match(source, /maxOutputQueueBytes = 2 << 20/);
  assert.match(source, /terminal\.write\(chunk, \(\) =>/);
  assert.match(source, /connection\?\.acknowledge\(chunk\.byteLength\)/);
  assert.match(source, /screenReaderMode: true/);
  assert.match(source, /addEventListener\('mousedown', handleNativeSelectionMouseDown, true\)/);
  assert.match(
    source,
    /function handleNativeSelectionMouseDown\(event: MouseEvent\) \{\s*if \(event\.button === 0\) event\.stopImmediatePropagation\(\);/,
  );
  assert.match(
    source,
    /function handleNativeSelectionClick\(\) \{\s*if \(!hasNativeTerminalSelection\(\)\) terminal\?\.focus\(\);/,
  );
  assert.match(source, /if \(hasNativeTerminalSelection\(\)\) \{\s*touchScrollY = null;\s*return;/);
  assert.match(source, /addEventListener\('touchmove', handleTouchMove, \{ passive: false \}\)/);
  assert.match(source, /terminal\.scrollLines\(lines\)/);
  assert.match(source, /label="Ctrl"[\s\S]*:aria-pressed="isModifierPressed\('ctrl'\)"/);
  assert.match(source, /pressedModifiers\.value = new Set\(\)/);
  assert.match(source, /sendKey\('\\u001b\[A'\)/);
  assert.match(source, /getComputedStyle\(terminalHost\.value \?\? document\.body\)/);
  assert.match(source, /background: color\('--ac-terminal-bg'\)/);
  assert.match(source, /brightWhite: color\('--ac-ansi-bright-white'\)/);
  assert.match(source, /\.terminal-view__host--native-selection\s*\{[^}]*touch-action:\s*auto/s);
  assert.match(
    source,
    /\.terminal-view__host--native-selection :deep\(\.xterm\)\s*\{[^}]*user-select:\s*text[^}]*-webkit-user-select:\s*text/s,
  );
  assert.match(
    source,
    /\.terminal-view__host--native-selection :deep\(\.xterm-accessibility:not\(\.debug\)\)\s*\{[^}]*pointer-events:\s*auto/s,
  );
  assert.match(
    source,
    /themeObserver\.observe\(document\.body, \{ attributes: true, attributeFilter: \['class'\] \}\)/,
  );
  assert.match(
    source,
    /themeObserver\.observe\(document\.documentElement, \{ attributes: true, attributeFilter: \['style'\] \}\)/,
  );
  assert.match(sessionView, /aria-label="Terminal 状态控制"/);
  assert.match(sessionView, /class="terminal-session-mobile-actions"[\s\S]*label="停止"/);
  assert.match(
    sessionView,
    /:style="\{ '--terminal-session-viewport-bottom': visualViewportBottom \}"/,
  );
  assert.match(
    sessionView,
    /window\.visualViewport\?\.addEventListener\('resize', scheduleVisualViewportSync\)/,
  );
  assert.match(
    sessionView,
    /window\.visualViewport\?\.removeEventListener\('resize', scheduleVisualViewportSync\)/,
  );
  assert.match(
    sessionView,
    /window\.visualViewport\?\.addEventListener\('scroll', scheduleVisualViewportSync\)/,
  );
  assert.match(sessionView, /viewport\.offsetTop \+ viewport\.height/);
  assert.match(
    sessionView,
    /height:\s*calc\(var\(--terminal-session-viewport-bottom, 100dvh\) - 50px\)/,
  );
  assert.match(sessionView, /\.terminal-session-card\s*\{[^}]*min-height:\s*0/s);
});

test('session detail and overview render terminal-specific surfaces', async () => {
  const [
    detail,
    detailView,
    horizontalTerminal,
    horizontalConversation,
    overview,
    horizontal,
    terminalButton,
    sessions,
    styles,
  ] = await Promise.all([
    read('src/pages/SessionDetailPage.vue'),
    read('src/components/SessionDetailView.vue'),
    read('src/components/OverviewHorizontalTerminal.vue'),
    read('src/components/OverviewHorizontalConversation.vue'),
    read('src/pages/IndexPage.vue'),
    read('src/components/OverviewHorizontalSession.vue'),
    read('src/components/SessionTerminalButton.vue'),
    read('src/services/sessions.ts'),
    read('src/css/app.scss'),
  ]);
  assert.match(detail, /mode === 'terminal'/);
  assert.match(detail, /TerminalSessionView/);
  assert.match(horizontal, /v-if="card\.mode === 'terminal'"/);
  assert.match(horizontalTerminal, /TerminalView/);
  assert.doesNotMatch(horizontalTerminal, /SessionDetailView|SessionTerminalButton/);
  assert.match(horizontalTerminal, /aria-label="启动 Terminal"/);
  assert.match(
    horizontalTerminal,
    /class="lane-icon-btn app-icon-btn"[\s\S]*color="primary"[\s\S]*aria-label="启动 Terminal"/,
  );
  assert.match(horizontalTerminal, /aria-label="停止 Terminal"/);
  assert.match(horizontalTerminal, /stopSession\(props\.card\.id\)/);
  assert.match(horizontalTerminal, /aria-label="关闭卡片"/);
  assert.match(horizontalTerminal, /executeSession\(props\.card\.id\)/);
  assert.match(horizontalTerminal, /:interactive="card\.status === 'running'"/);
  assert.match(
    horizontalConversation,
    /<SessionTerminalButton[\s\S]*:source-session-id="card\.id"/,
  );
  assert.match(
    detailView,
    /class="session-detail-tool-row"[\s\S]*<SessionTerminalButton[\s\S]*full-width[\s\S]*label="思维图"[\s\S]*:to="mindMapRoute"/,
  );
  assert.match(terminalButton, /openSessionTerminal\(props\.sourceSessionId\)/);
  assert.match(terminalButton, /emit\('opened', terminal\.id\)/);
  assert.match(terminalButton, /if \(props\.stayOnPage\) return/);
  assert.match(terminalButton, /name: 'session-detail'/);
  assert.match(
    horizontalConversation,
    /<SessionTerminalButton[\s\S]*stay-on-page[\s\S]*@opened="emit\('terminal-opened', \$event\)"/,
  );
  assert.match(overview, /@terminal-opened="refreshOverviewCard"/);
  assert.doesNotMatch(horizontal, /@contextmenu(?:\.|=)/);
  assert.match(sessions, /openSessionTerminal\(sessionId: \$sessionId\)/);
  assert.match(overview, /card\.terminalSummary\?\.currentDirectory/);
  assert.match(overview, /card\.terminalSummary\.commands/);
  assert.match(sessions, /terminalSummary \{\s*currentDirectory\s*commands\s*\}/);
  assert.doesNotMatch(horizontal, /card\.mode === 'terminal' \|\| sessionLayout === 'desktop'/);
  assert.match(horizontal, /<OverviewHorizontalConversation[\s\S]*:layout="sessionLayout"/);
  assert.match(overview, /:style-fn="isHorizontalView \? horizontalPageStyle : undefined"/);
  assert.match(styles, /\.page-shell\.workbench-page--horizontal\s*{[^}]*overflow:\s*hidden/s);
});

async function read(path) {
  return readFile(new URL(path, root), 'utf8');
}
