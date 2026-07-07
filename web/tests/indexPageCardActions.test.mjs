import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

test('overview cards rely on the card click target instead of a duplicate detail button', () => {
  const source = readFileSync(new URL('../src/pages/IndexPage.vue', import.meta.url), 'utf8');

  assert.equal(source.includes('aria-label="打开卡片"'), false);
  assert.equal(source.includes('打开卡片详情'), false);
});
