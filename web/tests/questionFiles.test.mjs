import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const panel = readFileSync(new URL('../src/components/QuestionsPanel.vue', import.meta.url), 'utf8');
const service = readFileSync(new URL('../src/services/sessions.ts', import.meta.url), 'utf8');

test('question files round-trip through the questions GraphQL contract', () => {
  assert.match(service, /questions\s*\{[\s\S]*?files\s*\{[\s\S]*?previewKind[\s\S]*?previewUrl/);
  assert.match(service, /files:\s*question\.files/);
});

test('question files render compactly below the body with hover and click previews', () => {
  const card = panel.match(/<q-card[^>]*class="question-card"[^>]*>[\s\S]*?<\/q-card>/)?.[0];
  assert.ok(card);
  assert.match(card, /question\.files\.length/);
  assert.match(card, /@click="openFilePreview\(file\)"/);
  assert.match(card, /class="question-file-thumbnail"/);
  assert.match(card, /<q-tooltip[\s\S]*?<SessionFilePreview/);
  assert.match(card, /@show="hoveredFileId = file\.id"/);
  assert.match(panel, /<q-dialog v-model="filePreviewOpen"/);
  assert.match(panel, /<SessionFilePreview :file="selectedFile"/);
  assert.match(panel, /aria-label="关闭文件预览"/);
  assert.match(panel, /@media \(hover: hover\)/);
  assert.match(panel, /@media \(max-width: 599\.98px\)/);
});
