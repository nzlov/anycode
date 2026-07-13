import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const stylesSource = readSource('../src/css/app.scss');
const newSessionSource = readSource('../src/components/NewSessionDialog.vue');
const globalSettingsSource = readSource('../src/components/GlobalSettingsDialog.vue');
const projectSettingsSource = readSource('../src/components/ProjectSettingsDialog.vue');
const directorySource = readSource('../src/components/ProjectDirectoryDialog.vue');
const answerSource = readSource('../src/components/AnswerUserDialog.vue');
const composerSource = readSource('../src/components/PromptComposer.vue');
const indexSource = readSource('../src/pages/IndexPage.vue');
const detailSource = readSource('../src/pages/SessionDetailPage.vue');
const layoutSource = readSource('../src/layouts/MainLayout.vue');

const contentDialogs = [
  [newSessionSource, 'new-session-dialog'],
  [globalSettingsSource, 'global-settings-dialog'],
  [projectSettingsSource, 'project-settings-dialog'],
  [directorySource, 'directory-dialog'],
  [answerSource, 'answer-dialog'],
  [indexSource, 'forward-approval-dialog'],
  [indexSource, 'overview-diff-dialog'],
  [composerSource, 'attachment-preview-card'],
  [detailSource, 'prompt-edit-dialog'],
  [detailSource, 'file-diff-dialog'],
];

test('one shared class owns content dialog sizing at the 600px breakpoint', () => {
  assert.match(
    stylesSource,
    /\.app-content-dialog\s*{[^}]*width:\s*90vw\s*!important[^}]*max-width:\s*90vw\s*!important[^}]*max-height:\s*90dvh/s,
  );
  assert.match(
    stylesSource,
    /@media \(max-width:\s*599\.98px\)[\s\S]*?\.app-content-dialog\s*{[^}]*width:\s*100vw\s*!important[^}]*height:\s*100dvh[^}]*max-height:\s*100dvh[^}]*border-radius:\s*0/s,
  );
  assert.doesNotMatch(stylesSource, /\.app-content-dialog\s*{[^}]*max-width:\s*\d+px/s);
});

test('all content dialogs use the shared card class and Quasar mobile maximization', () => {
  for (const [source, semanticClass] of contentDialogs) {
    assert.match(
      source,
      new RegExp(`class="[^"]*\\b${semanticClass}\\b[^"]*\\bapp-content-dialog\\b[^"]*"`),
      `${semanticClass} must use app-content-dialog`,
    );
  }

  for (const source of [
    newSessionSource,
    globalSettingsSource,
    projectSettingsSource,
    directorySource,
    answerSource,
    indexSource,
    composerSource,
  ]) {
    assert.match(source, /<q-dialog[\s\S]{0,180}:maximized="\$q\.screen\.lt\.sm"/);
  }
  assert.equal(
    (detailSource.match(/<q-dialog[^>]*:maximized="\$q\.screen\.lt\.sm"/g) ?? []).length,
    2,
  );
});

test('compact confirmation dialogs do not use the content dialog contract', () => {
  assert.equal((layoutSource.match(/class="confirm-dialog"/g) ?? []).length, 2);
  assert.doesNotMatch(layoutSource, /class="[^"]*confirm-dialog[^"]*app-content-dialog/);
});

test('semantic dialog styles no longer own fixed dialog widths or old mobile breakpoints', () => {
  const semanticClasses = contentDialogs.map(([, semanticClass]) => semanticClass).join('|');
  const semanticBlocks = new RegExp(`\\.(?:${semanticClasses})\\s*{[^}]*}`, 'g');
  for (const block of stylesSource.match(semanticBlocks) ?? []) {
    assert.doesNotMatch(block, /(?:width|max-width):[^;]*(?:560|680|760|880|900|920|960|1100)px/);
  }
  assert.doesNotMatch(answerSource, /@media \(max-width:\s*699px\)/);
  assert.doesNotMatch(detailSource, /@media \(max-width:\s*699px\)[\s\S]*?\.file-diff-dialog/);
});

test('long content dialogs keep one explicit scrolling content area', () => {
  assert.match(stylesSource, /\.new-session-body\s*{[^}]*overflow-y:\s*auto/s);
  assert.match(stylesSource, /\.quick-command-list\s*{[^}]*overflow-y:\s*auto/s);
  assert.match(answerSource, /\.answer-dialog__body\s*{[^}]*overflow:\s*hidden/s);
  assert.match(stylesSource, /\.forward-approval-dialog__panel\s*{[^}]*overflow:\s*auto/s);
  assert.match(detailSource, /\.file-diff-body\s*{[^}]*overflow:\s*auto/s);
});
