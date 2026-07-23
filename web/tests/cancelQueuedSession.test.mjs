import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

test('queued sessions expose cancel actions without removing force start', () => {
  const overviewSource = readSource('../src/pages/IndexPage.vue');
  const detailSource = readSource('../src/components/SessionDetailView.vue');
  const tableSource = readSource('../src/pages/SessionsPage.vue');

  assert.match(overviewSource, /card\.status === 'queued'[\s\S]*取消排队/);
  assert.match(overviewSource, /@click\.stop="cancelQueuedCard\(card\)"/);
  assert.match(overviewSource, /tooltip: '强制启动排队卡片'/);
  assert.match(overviewSource, /catch \{[\s\S]*await refreshOverviewCard\(card\.id\)/);

  assert.match(detailSource, /const canCancelQueue = computed/);
  assert.match(detailSource, /v-if="canCancelQueue"[\s\S]*aria-label="取消排队"/);
  assert.match(detailSource, /@click="stopSession"/);
  assert.match(detailSource, /run: executeWithComposerConfig/);

  assert.match(tableSource, /import \{[^}]*stopSession[^}]*\} from '@\/services\/sessions'/s);
  assert.match(tableSource, /props\.row\.status === 'queued'[\s\S]*aria-label="取消排队"/);
  assert.match(tableSource, /@click\.stop="cancelQueuedSession\(props\.row\)"/);
  assert.match(tableSource, /catch \{[\s\S]*loadSessions\(\)\.catch/);
});
