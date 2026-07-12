import assert from 'node:assert/strict';
import { test } from 'node:test';

import { formatTokenCount } from '../src/services/sessionTimelinePresentation.ts';

test('formatTokenCount converts token counts to compact decimal units', () => {
  assert.equal(formatTokenCount(999), '999');
  assert.equal(formatTokenCount(1_000), '1K');
  assert.equal(formatTokenCount(12_500), '12.5K');
  assert.equal(formatTokenCount(999_999), '1M');
  assert.equal(formatTokenCount(2_000_000), '2M');
  assert.equal(formatTokenCount(1_500_000_000), '1.5B');
});
