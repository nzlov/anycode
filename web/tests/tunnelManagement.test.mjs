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
  assert.match(manager, /\{\{ tunnel\.accessUrl \}\}/);
  assert.doesNotMatch(manager, /createTunnel/);
  assert.match(service, /query Tunnels/);
  assert.match(service, /mutation CloseTunnel/);
});
