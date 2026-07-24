import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const layout = readSource('../src/layouts/MainLayout.vue');
const routes = readSource('../src/router/routes.ts');
const manager = readSource('../src/components/TunnelManagerDialog.vue');
const service = readSource('../src/services/tunnels.ts');
const button = readSource('../src/components/SessionTunnelButton.vue');
const index = readSource('../src/pages/IndexPage.vue');
const horizontal = readSource('../src/components/OverviewHorizontalSession.vue');
const horizontalDesktop = readSource('../src/components/OverviewHorizontalSessionDesktop.vue');
const horizontalMobile = readSource('../src/components/OverviewHorizontalSessionMobile.vue');
const detail = readSource('../src/components/SessionDetailView.vue');

test('tunnel entry follows history and uses page navigation only on mobile', () => {
  assert.match(layout, /aria-label="历史卡片"[\s\S]*aria-label="隧道"/);
  assert.match(
    layout,
    /function openTunnels[\s\S]*\$q\.screen\.lt\.sm[\s\S]*name: 'tunnels'[\s\S]*tunnelDialogOpen\.value = true/,
  );
  assert.match(routes, /path: 'tunnels', name: 'tunnels'/);
});

test('tunnel manager only lists and closes active tunnels', () => {
  assert.match(manager, /listTunnels\(\)/);
  assert.match(manager, /closeTunnelRequest\(tunnel\.id\)/);
  assert.match(manager, /:href="tunnel\.accessUrl"/);
  assert.match(manager, /\{\{ tunnel\.name \}\}/);
  assert.match(manager, /v-for="group in tunnelGroups"/);
  assert.match(manager, /getSessionCard\(sessionId, \{ notify: false \}\)/);
  assert.doesNotMatch(manager, /createTunnel/);
  assert.match(service, /query Tunnels/);
  assert.match(service, /sessionId[\s\S]*name[\s\S]*port/);
  assert.match(service, /mutation CloseTunnel/);
});

test('session tunnel entry opens one tunnel directly and offers named choices for multiple', () => {
  assert.match(button, /v-if="tunnels\.length"/);
  assert.match(button, /if \(props\.tunnels\.length !== 1\) return/);
  assert.match(button, /window\.open\(tunnel\.accessUrl, '_blank', 'noopener,noreferrer'\)/);
  assert.match(button, /<q-menu v-else/);
  assert.match(button, /\{\{ tunnel\.name \}\}/);

  assert.match(index, /<SessionTunnelButton :tunnels="tunnelsForSession\(card\.id\)" show-count/);
  assert.match(horizontal, /:tunnels="tunnels"/);
  assert.match(horizontalDesktop, /<SessionTunnelButton :tunnels="tunnels"/);
  assert.match(horizontalMobile, /<SessionTunnelButton :tunnels="tunnels"/);
});

test('session information shows named tunnels below token usage and removes obsolete blocks', () => {
  assert.match(detail, /<q-item-label caption>Token 用量<\/q-item-label>[\s\S]*<q-item-label caption>隧道<\/q-item-label>/);
  assert.match(detail, /v-for="tunnel in sessionTunnels"[\s\S]*\{\{ tunnel\.name \}\}/);
  assert.doesNotMatch(detail, /<q-item-label caption>工作树清理<\/q-item-label>/);
  assert.doesNotMatch(detail, /<q-item-label caption>状态<\/q-item-label>/);
  assert.doesNotMatch(detail, /<q-item-label caption>权限<\/q-item-label>/);
});
