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

test('terminal view covers resize, processed-output acknowledgement, and mobile control keys', async () => {
  const source = await read('src/components/TerminalView.vue');
  assert.match(source, /new ResizeObserver\(fitTerminal\)/);
  assert.match(source, /terminal\.onData/);
  assert.match(source, /interactive\?: boolean/);
  assert.match(source, /maxOutputQueueBytes = 2 << 20/);
  assert.match(source, /terminal\.write\(chunk, \(\) =>/);
  assert.match(source, /connection\?\.acknowledge\(chunk\.byteLength\)/);
  assert.match(source, /Ctrl-C/);
  assert.match(source, /sendKey\('\\u001b\[A'\)/);
});

test('session detail and overview render terminal-specific surfaces', async () => {
  const [
    detail,
    detailView,
    desktop,
    mobile,
    overview,
    horizontal,
    terminalButton,
    sessions,
    styles,
  ] = await Promise.all([
    read('src/pages/SessionDetailPage.vue'),
    read('src/components/SessionDetailView.vue'),
    read('src/components/OverviewHorizontalSessionDesktop.vue'),
    read('src/components/OverviewHorizontalSessionMobile.vue'),
    read('src/pages/IndexPage.vue'),
    read('src/components/OverviewHorizontalSession.vue'),
    read('src/components/SessionTerminalButton.vue'),
    read('src/services/sessions.ts'),
    read('src/css/app.scss'),
  ]);
  assert.match(detail, /mode === 'terminal'/);
  assert.match(detail, /TerminalSessionView/);
  assert.match(desktop, /card\.mode === 'terminal'/);
  assert.match(desktop, /TerminalView/);
  assert.match(desktop, /v-if="card\.mode !== 'terminal'"/);
  assert.match(desktop, /aria-label="启动 Terminal"/);
  assert.match(
    desktop,
    /class="lane-icon-btn app-icon-btn"[\s\S]*color="primary"[\s\S]*aria-label="启动 Terminal"/,
  );
  assert.match(desktop, /aria-label="停止 Terminal"/);
  assert.match(desktop, /stopSession\(props\.card\.id\)/);
  assert.match(desktop, /aria-label="关闭卡片"/);
  assert.match(desktop, /executeSession\(props\.card\.id\)/);
  assert.match(desktop, /:interactive="card\.status === 'running'"/);
  assert.match(desktop, /<SessionTerminalButton[\s\S]*:source-session-id="card\.id"/);
  assert.match(mobile, /<SessionTerminalButton[\s\S]*:source-session-id="card\.id"/);
  assert.match(
    detailView,
    /<SessionTerminalButton[\s\S]*full-width[\s\S]*<q-btn[\s\S]*label="关闭卡片"/,
  );
  assert.match(terminalButton, /openSessionTerminal\(props\.sourceSessionId\)/);
  assert.match(terminalButton, /emit\('opened', terminal\.id\)/);
  assert.match(terminalButton, /if \(props\.stayOnPage\) return/);
  assert.match(terminalButton, /name: 'session-detail'/);
  assert.match(
    desktop,
    /<SessionTerminalButton[\s\S]*stay-on-page[\s\S]*@opened="emit\('terminal-opened', \$event\)"/,
  );
  assert.match(
    mobile,
    /<SessionTerminalButton[\s\S]*stay-on-page[\s\S]*@opened="emit\('terminal-opened', \$event\)"/,
  );
  assert.match(overview, /@terminal-opened="refreshOverviewCard"/);
  assert.doesNotMatch(horizontal, /@contextmenu(?:\.|=)/);
  assert.match(sessions, /openSessionTerminal\(sessionId: \$sessionId\)/);
  assert.match(overview, /card\.terminalSummary\?\.currentDirectory/);
  assert.match(overview, /card\.terminalSummary\.commands/);
  assert.match(sessions, /terminalSummary \{\s*currentDirectory\s*commands\s*\}/);
  assert.match(horizontal, /card\.mode === 'terminal' \|\| sessionLayout === 'desktop'/);
  assert.match(overview, /:style-fn="isHorizontalView \? horizontalPageStyle : undefined"/);
  assert.match(styles, /\.page-shell\.workbench-page--horizontal\s*{[^}]*overflow:\s*hidden/s);
});

async function read(path) {
  return readFile(new URL(path, root), 'utf8');
}
