import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

test('global settings expose an explicitly saved Codex context window', () => {
  const settingsSource = readSource('../src/components/GlobalSettingsDialog.vue');
  const serviceSource = readSource('../src/services/codexSettings.ts');

  assert.match(settingsSource, /name="codex"[^>]*label="Codex"/);
  assert.match(settingsSource, /activeSection === 'codex'/);
  assert.match(settingsSource, /上下文长度/);
  assert.match(settingsSource, /留空时跟随模型默认值/);
  assert.match(settingsSource, /label="保存"[\s\S]*@click="saveCodexSettings"/);
  assert.doesNotMatch(
    settingsSource,
    /watch\([\s\S]*codexContextWindowInput[\s\S]*saveCodexSettings/,
  );
  assert.match(serviceSource, /query CodexSettings/);
  assert.match(serviceSource, /mutation UpdateCodexSettings/);
  assert.match(serviceSource, /contextWindow: number \| null/);
});
