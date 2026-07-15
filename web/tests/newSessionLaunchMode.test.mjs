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
const stylesSource = readSource('../src/css/app.scss');

test('launch mode buttons replace the separate mode toggle and create command', () => {
  assert.doesNotMatch(dialogSource, /<q-btn-toggle[\s\S]*class="new-session-mode"/);
  assert.doesNotMatch(dialogSource, /label="创建卡片"/);
  assert.match(dialogSource, /label="流程模式"[\s\S]*@click="createSession\('workflow'\)"/);
  assert.match(dialogSource, /label="会话模式"[\s\S]*@click="createSession\('chat'\)"/);
  assert.match(dialogSource, /preferredAvailableMode === 'workflow' \? 'positive'/);
  assert.match(dialogSource, /preferredAvailableMode === 'chat' \? 'positive'/);
  assert.match(dialogSource, /class="new-session-launch-group" role="group" aria-label="启动模式"/);
  assert.match(
    stylesSource,
    /\.new-session-launch-group\s*{[^}]*grid-auto-columns:\s*minmax\(116px,\s*1fr\)[^}]*gap:\s*1px/s,
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
    /await createSessionRequest\(input\);[\s\S]*rememberLaunchMode\(input\.mode\)/,
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
  assert.equal((controlsSource.match(/dropdown-icon=""/g) ?? []).length, 3);
  assert.match(
    stylesSource,
    /\.toolbar-file-picker \.q-field__prepend\s*{[^}]*justify-content:\s*center/s,
  );
  assert.match(stylesSource, /\.permission-select\s*{[^}]*width:\s*44px/s);
  assert.match(controlsSource, /class="compact-select model-select"/);
  assert.match(controlsSource, /class="compact-select effort-select"/);
  assert.match(
    dialogSource,
    /:force-config-menu="\s*\$q\.screen\.lt\.md \|\| \(panel && \$q\.screen\.width < overviewInlineConfigMinWidth\)\s*"/,
  );
});
