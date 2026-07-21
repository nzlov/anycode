import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

test('Quasar builds the application in PWA InjectManifest mode', () => {
  const packageSource = readSource('../package.json');
  const packageJSON = JSON.parse(packageSource);
  const configSource = readSource('../quasar.config.ts');
  const indexSource = readSource('../index.html');
  const registerSource = readSource('../src-pwa/register-sw.ts');

  assert.equal(packageJSON.scripts.build, 'quasar build -m pwa');
  assert.equal(packageJSON.scripts.dev, 'quasar dev -m pwa');
  assert.match(configSource, /workboxMode: 'InjectManifest'/);
  assert.match(configSource, /pwaServiceWorker: 'src-pwa\/sw\/custom-sw'/);
  assert.match(configSource, /injectPWAMetaTags: false/);
  assert.doesNotMatch(indexSource, /rel="manifest"/);
  assert.match(indexSource, /rel="apple-touch-icon" href="icons\/favicon-128x128\.png"/);
  assert.match(registerSource, /register\(import\.meta\.env\.QUASAR_SERVICE_WORKER_FILE/);
});

test('PWA output is the sole frontend artifact embedded by Go', () => {
  const packageJSON = JSON.parse(readSource('../package.json'));
  const configSource = readSource('../quasar.config.ts');
  const staticSource = readSource('../../internal/interfaces/http/static/static.go');
  const serverSource = readSource('../../internal/interfaces/http/server.go');
  const dockerSource = readSource('../../Dockerfile');

  assert.equal(packageJSON.scripts.postbuild, 'node scripts/ensure-pwa-gitkeep.mjs');
  assert.match(configSource, /distDir: '\.\.\/internal\/interfaces\/http\/static\/pwa'/);
  assert.match(staticSource, /const PWADir = "pwa"/);
  assert.match(staticSource, /\/\/go:embed all:pwa/);
  assert.match(serverSource, /fs\.Sub\(static\.Files, static\.PWADir\)/);
  assert.match(dockerSource, /static\/pwa \.\/internal\/interfaces\/http\/static\/pwa/);

  for (const source of [configSource, staticSource, serverSource, dockerSource]) {
    assert.doesNotMatch(source, /internal\/interfaces\/http\/static\/dist|static\.DistDir/);
  }
});

test('service worker shows card notifications and opens their hash route', () => {
  const workerSource = readSource('../src-pwa/sw/custom-sw.ts');

  assert.match(workerSource, /addEventListener\('push'/);
  assert.match(workerSource, /registration\.showNotification/);
  assert.match(workerSource, /addEventListener\('notificationclick'/);
  assert.match(workerSource, /clients\.openWindow\(target\)/);
  assert.match(workerSource, /current\.hash === target\.hash/);
});

test('browser subscription is server-owned and rotates with the persisted VAPID public key', () => {
  const serviceSource = readSource('../src/services/pushNotifications.ts');
  const settingsSource = readSource('../src/components/GlobalSettingsDialog.vue');

  assert.match(serviceSource, /query WebPushConfig/);
  assert.match(serviceSource, /mutation RegisterPushSubscription/);
  assert.match(serviceSource, /mutation UnregisterPushSubscription/);
  assert.match(serviceSource, /!subscriptionUsesKey\(subscription, config\.publicKey\)/);
  assert.match(serviceSource, /applicationServerKey: base64URLBytes\(config\.publicKey\)/);
  assert.match(settingsSource, /卡片系统通知/);
  assert.match(settingsSource, /<q-toggle/);
});
