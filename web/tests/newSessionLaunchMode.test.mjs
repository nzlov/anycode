import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const dialogSource = readSource('../src/components/NewSessionDialog.vue');
const composerSource = readSource('../src/components/PromptComposer.vue');
const codexComposerSource = readSource('../src/components/CodexPromptComposer.vue');
const controlsSource = readSource('../src/components/PromptConfigControls.vue');
const modelSelectorSource = readSource('../src/components/CodexModelSelector.vue');
const stylesSource = readSource('../src/css/app.scss');

test('launch mode buttons replace the separate mode toggle and create command', () => {
  assert.doesNotMatch(dialogSource, /<q-btn-toggle[\s\S]*class="new-session-mode"/);
  assert.doesNotMatch(dialogSource, /label="创建卡片"/);
  assert.match(dialogSource, /label="流程模式"[\s\S]*@click="createSession\('workflow'\)"/);
  assert.match(dialogSource, /label="会话模式"[\s\S]*@click="createSession\('chat'\)"/);
  assert.match(dialogSource, /preferredAvailableMode === 'workflow' \? 'primary'/);
  assert.match(dialogSource, /preferredAvailableMode === 'chat' \? 'primary'/);
  assert.match(dialogSource, /class="new-session-launch-group" role="group" aria-label="启动模式"/);
  assert.match(
    stylesSource,
    /\.new-session-launch-group\s*{[^}]*grid-auto-columns:\s*minmax\(116px,\s*1fr\)[^}]*gap:\s*0/s,
  );
  assert.match(
    stylesSource,
    /\.new-session-launch-btn:first-child\s*{[^}]*border-radius:\s*calc\(var\(--ac-radius\)\s*-\s*1px\)\s+0\s+0\s+calc\(var\(--ac-radius\)\s*-\s*1px\)/s,
  );
  assert.match(
    stylesSource,
    /\.new-session-launch-btn:last-child\s*{[^}]*border-radius:\s*0\s+calc\(var\(--ac-radius\)\s*-\s*1px\)\s+calc\(var\(--ac-radius\)\s*-\s*1px\)\s+0/s,
  );
});

test('successful launches remember the mode without project availability changing it', () => {
  assert.match(dialogSource, /anycode\.lastNewSessionLaunchMode/);
  assert.match(
    dialogSource,
    /const preferredMode = ref<'workflow' \| 'chat'>\(storedLaunchMode\(\)\)/,
  );
  assert.match(
    dialogSource,
    /await createSessionRequest\(input\);[\s\S]*rememberLaunchMode\(input\.mode === 'workflow' \? 'workflow' : 'chat'\)/,
  );

  const availabilityBody = dialogSource.match(
    /async function loadWorkflowAvailability\(\) \{(?<body>[\s\S]*?)\n\}/,
  )?.groups?.body;
  assert.ok(availabilityBody);
  assert.doesNotMatch(availabilityBody, /preferredMode\.value|rememberLaunchMode/);
  assert.match(
    dialogSource,
    /preferredMode\.value === 'workflow' && canUseWorkflowMode\.value \? 'workflow' : 'chat'/,
  );
});

test('Shift+Enter submits the preferred available mode through the shared composer', () => {
  assert.match(composerSource, /@keydown\.shift\.enter\.prevent="emit\('submit'\)"/);
  assert.match(composerSource, /submit: \[\]/);
  assert.match(codexComposerSource, /@submit="emit\('submit'\)"/);
  assert.match(dialogSource, /@submit="createSession\(preferredAvailableMode\)"/);
  assert.match(
    dialogSource,
    /async function createSession\(requestedMode: 'workflow' \| 'chat'\) \{\s*if \(creating\.value\) return;/,
  );
});

test('prompt toolbar controls use the compact icon and label treatment', () => {
  assert.match(controlsSource, /class="compact-select permission-select"/);
  assert.match(controlsSource, /运行权限：\{\{ permissionLabel \}\}/);
  assert.doesNotMatch(controlsSource, /name="smart_toy"|name="psychology"/);
  assert.equal((controlsSource.match(/hide-dropdown-icon/g) ?? []).length, 1);
  assert.equal((modelSelectorSource.match(/hide-dropdown-icon/g) ?? []).length, 2);
  assert.doesNotMatch(controlsSource, /dropdown-icon=""/);
  assert.match(composerSource, /class="app-icon-btn toolbar-file-picker"/);
  assert.match(stylesSource, /\.permission-select\s*{[^}]*width:\s*44px/s);
  assert.match(controlsSource, /<CodexModelSelector/);
  assert.match(modelSelectorSource, /class="compact-select model-select"/);
  assert.match(modelSelectorSource, /class="compact-select effort-select"/);
  assert.match(
    stylesSource,
    /\.compact-select\s*{[^}]*width:\s*max-content[^}]*max-width:\s*20ch/s,
  );
  assert.doesNotMatch(stylesSource, /\.compact-select\s*{[^}]*min-width:\s*150px/s);
  assert.doesNotMatch(
    stylesSource,
    /\.new-session-dialog--panel \.(?:model|effort)-select\s*{[^}]*(?:width|min-width):/s,
  );
  assert.match(modelSelectorSource, /Codex 模型：\{\{ modelLabel \}\}/);
  assert.match(modelSelectorSource, /思考强度：\{\{ effortLabel \}\}/);
  assert.match(
    dialogSource,
    /:force-config-menu="\s*\$q\.screen\.lt\.md \|\| \(panel && \$q\.screen\.width < overviewInlineConfigMinWidth\)\s*"/,
  );
  assert.match(stylesSource, /^\.prompt-toolbar\s*{[^}]*gap:\s*5px/ms);
  assert.match(stylesSource, /^\.prompt-config-controls\s*{[^}]*gap:\s*5px/ms);
  assert.match(stylesSource, /^\.prompt-config-controls--stacked\s*{[^}]*gap:\s*5px/ms);
  assert.match(
    stylesSource,
    /^\.new-session-dialog--panel \.prompt-toolbar\s*{[^}]*gap:\s*5px/ms,
  );
  assert.match(
    stylesSource,
    /^\.new-session-dialog--panel \.prompt-config-controls\s*{[^}]*gap:\s*5px/ms,
  );
});
