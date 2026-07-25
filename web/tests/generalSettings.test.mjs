import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

test('global concurrency is database-backed and editable in general settings', () => {
  const serviceSource = readSource('../src/services/generalSettings.ts');
  const settingsSource = readSource('../src/components/GlobalSettingsDialog.vue');
  const configSource = readSource('../../internal/infra/config/config.go');

  assert.match(serviceSource, /query GeneralSettings/);
  assert.match(serviceSource, /mutation UpdateGeneralSettings/);
  assert.match(settingsSource, /name="general"/);
  assert.match(settingsSource, /Agent 并发数量/);
  assert.match(settingsSource, /general\.agentMaxConcurrent/);
  assert.doesNotMatch(configSource, /ANYCODE_AGENT_MAX_CONCURRENT/);
});
