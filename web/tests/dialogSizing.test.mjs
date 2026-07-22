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
const questionsSource = readSource('../src/components/QuestionsDialog.vue');
const diffWorkspaceSource = readSource('../src/components/DiffWorkspace.vue');
const composerSource = readSource('../src/components/PromptComposer.vue');
const indexSource = readSource('../src/pages/IndexPage.vue');
const detailSource = readSource('../src/components/SessionDetailView.vue');
const promptEditSource = readSource('../src/components/PromptAppendEditPanel.vue');
const layoutSource = readSource('../src/layouts/MainLayout.vue');
const routesSource = readSource('../src/router/routes.ts');
const settingsPageSource = readSource('../src/pages/SettingsPage.vue');
const newSessionPageSource = readSource('../src/pages/NewSessionPage.vue');

const contentDialogs = [
  [newSessionSource, 'new-session-dialog'],
  [globalSettingsSource, 'global-settings-dialog'],
  [projectSettingsSource, 'project-settings-dialog'],
  [directorySource, 'directory-dialog'],
  [questionsSource, 'questions-dialog'],
  [indexSource, 'forward-approval-dialog'],
  [indexSource, 'overview-diff-dialog'],
  [composerSource, 'attachment-preview-card'],
  [promptEditSource, 'prompt-edit-dialog'],
  [detailSource, 'event-resource-dialog'],
];

test('one shared class keeps fallback content dialogs compact at the 600px breakpoint', () => {
  assert.match(
    stylesSource,
    /\.app-content-dialog\s*{[^}]*width:\s*90vw\s*!important[^}]*max-width:\s*90vw\s*!important[^}]*max-height:\s*90dvh/s,
  );
  assert.match(
    stylesSource,
    /@media \(max-width:\s*599\.98px\)[\s\S]*?\.app-content-dialog\s*{[^}]*width:\s*calc\(100vw - 24px\)\s*!important[^}]*height:\s*auto\s*!important[^}]*max-height:\s*calc\(100dvh - 24px\)\s*!important/s,
  );
  assert.match(stylesSource, /\.surface-page \.app-content-dialog\s*{[^}]*width:\s*100%[^}]*max-height:\s*none/s);
  assert.doesNotMatch(stylesSource, /\.app-content-dialog\s*{[^}]*max-width:\s*\d+px/s);
});

test('content dialogs keep shared cards while mobile entries use route pages', () => {
  for (const [source, semanticClass] of contentDialogs) {
    assert.match(
      source,
      new RegExp(`class="[^"]*\\b${semanticClass}\\b[^"]*\\bapp-content-dialog\\b[^"]*"`),
      `${semanticClass} must use app-content-dialog`,
    );
  }

  assert.doesNotMatch(
    [newSessionSource, globalSettingsSource, projectSettingsSource, directorySource, questionsSource, indexSource, composerSource, detailSource].join('\n'),
    /:maximized=/,
  );
  assert.match(routesSource, /name: 'new-session'/);
  assert.match(routesSource, /name: 'settings'/);
  assert.match(newSessionPageSource, /<NewSessionDialog[\s\S]*page/);
  assert.match(settingsPageSource, /<GlobalSettingsDialog page/);
  assert.match(indexSource, /if \(\$q\.screen\.lt\.sm\)[\s\S]*name: 'new-session'/);
  assert.match(layoutSource, /if \(\$q\.screen\.lt\.sm\)[\s\S]*name: 'settings'/);
});

test('compact confirmation dialogs do not use the content dialog contract', () => {
  assert.equal((layoutSource.match(/class="confirm-dialog"/g) ?? []).length, 1);
  assert.equal((globalSettingsSource.match(/class="confirm-dialog"/g) ?? []).length, 1);
  for (const source of [layoutSource, globalSettingsSource]) {
    assert.doesNotMatch(source, /class="[^"]*confirm-dialog[^"]*app-content-dialog/);
  }
});

test('semantic dialog styles no longer own fixed dialog widths or old mobile breakpoints', () => {
  const semanticClasses = contentDialogs.map(([, semanticClass]) => semanticClass).join('|');
  const semanticBlocks = new RegExp(`\\.(?:${semanticClasses})\\s*{[^}]*}`, 'g');
  for (const block of stylesSource.match(semanticBlocks) ?? []) {
    assert.doesNotMatch(block, /(?:width|max-width):[^;]*(?:560|680|760|880|900|920|960|1100)px/);
  }
  assert.doesNotMatch(questionsSource, /@media \(max-width:\s*699px\)/);
  assert.doesNotMatch(detailSource, /file-diff-dialog/);
});

test('long content dialogs keep one explicit scrolling content area', () => {
  assert.match(
    stylesSource,
    /\.new-session-body\s*{[^}]*align-content:\s*start[^}]*overflow-y:\s*auto/s,
  );
  assert.match(stylesSource, /\.quick-command-list\s*{[^}]*overflow-y:\s*auto/s);
  assert.match(questionsSource, /\.questions-dialog__body\s*{[^}]*overflow:\s*hidden/s);
  assert.match(stylesSource, /\.forward-approval-dialog__panel\s*{[^}]*overflow:\s*auto/s);
  assert.match(diffWorkspaceSource, /\.diff-files\s*{[^}]*overflow-y:\s*auto/s);
  assert.match(diffWorkspaceSource, /container-type:\s*inline-size/);
});

test('desktop new session panel is capped to the viewport and scrolls its body', () => {
  assert.match(
    stylesSource,
    /\.new-session-dialog--panel\s*{[^}]*max-height:\s*var\(--overview-create-panel-height\)\s*!important/s,
  );
  assert.match(
    stylesSource,
    /\.new-session-dialog--panel \.new-session-body\s*{[^}]*flex:\s*1 1 auto[^}]*overflow-y:\s*auto/s,
  );
});
