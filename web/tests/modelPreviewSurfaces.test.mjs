import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const diffPreview = readFileSync(
  new URL('../src/components/DiffMediaPreview.vue', import.meta.url),
  'utf8',
);
const questionsPanel = readFileSync(
  new URL('../src/components/QuestionsPanel.vue', import.meta.url),
  'utf8',
);
const artifactEvent = readFileSync(
  new URL('../src/components/SessionArtifactEvent.vue', import.meta.url),
  'utf8',
);

test('all multimedia preview surfaces recognize 3D models', () => {
  assert.match(diffPreview, /kind === 'model'/);
  assert.match(diffPreview, /<ModelFilePreview/);
  assert.match(questionsPanel, /file\.previewKind === 'model'/);
  assert.match(artifactEvent, /'model'/);
});
