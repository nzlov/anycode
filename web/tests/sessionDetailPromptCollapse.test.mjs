import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const promptSource = readSource('../src/components/PromptComposer.vue');
const codexPromptSource = readSource('../src/components/CodexPromptComposer.vue');
const detailSource = readSource('../src/pages/SessionDetailPage.vue');
const stylesSource = readSource('../src/css/app.scss');

test('session detail prompt starts collapsed with an expanding keyboard control', () => {
  assert.match(detailSource, /v-model:collapsed="composerCollapsed"/);
  assert.match(detailSource, /const composerCollapsed = ref\(true\)/);
  assert.match(
    detailSource,
    /'detail-composer--collapsed':\s*composerCollapsed && !isWaitingForAnswer && !isWaitingForApproval/,
  );
  assert.match(
    detailSource,
    /\.detail-composer--collapsed\s*{[^}]*min-height:\s*0/s,
  );
  assert.match(promptSource, /v-if="isCollapsed" class="prompt-shell__collapsed"/);
  assert.match(promptSource, /icon="keyboard"[\s\S]*aria-label="展开提示词"/);
  assert.match(
    stylesSource,
    /\.prompt-shell__expand\s*{[^}]*min-width:\s*44px[^}]*flex:\s*1 1 auto/s,
  );
  assert.match(codexPromptSource, /promptCollapsed \? '快捷指令'/);
});

test('expanded session detail prompt can collapse before the attachment control', () => {
  assert.match(promptSource, /icon="keyboard_hide"[\s\S]*aria-label="收起提示词"[\s\S]*<q-file/);
  assert.match(promptSource, /@click="emit\('update:collapsed', true\)"/);
  assert.match(codexPromptSource, /if \(props\.collapsible\) emit\('update:collapsed', false\)/);
});

test('successful prompt submission clears the draft and collapses the composer', () => {
  assert.match(
    detailSource,
    /await appendDescription\([\s\S]*appendArtifacts\.value = \[\];\s*composerCollapsed\.value = true/,
  );
});
