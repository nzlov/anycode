import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  defaultReasoningEffortForModel,
  firstCodexModelValue,
  normalizeCodexModel,
  normalizeReasoningEffort,
  reasoningEffortOptionsForModel,
} from '../src/components/promptOptions.ts';

const dynamicOptions = [
  {
    label: 'GPT-5.6-Sol',
    value: 'gpt-5.6-sol',
    defaultReasoningEffort: 'low',
    reasoningEfforts: [
      { label: 'low', value: 'low', description: 'Fast responses' },
      { label: 'ultra', value: 'ultra', description: 'Delegated maximum' },
    ],
  },
];

test('Codex model defaults come from the loaded model catalog', () => {
  assert.equal(firstCodexModelValue(dynamicOptions), 'gpt-5.6-sol');
  assert.equal(normalizeCodexModel(dynamicOptions, 'missing-model'), 'gpt-5.6-sol');
  assert.deepEqual(
    reasoningEffortOptionsForModel(dynamicOptions, 'gpt-5.6-sol').map((option) => option.value),
    ['low', 'ultra'],
  );
});

test('model changes use the next model default reasoning effort', () => {
  const nextModel = 'gpt-5.6-sol';

  assert.equal(defaultReasoningEffortForModel(dynamicOptions, nextModel), 'low');
  assert.equal(normalizeReasoningEffort(dynamicOptions, nextModel, 'missing-effort'), 'low');
});

test('normalization preserves values while the catalog is still loading', () => {
  assert.equal(normalizeCodexModel([], 'gpt-5.6-sol'), 'gpt-5.6-sol');
  assert.equal(normalizeReasoningEffort([], 'gpt-5.6-sol', 'ultra'), 'ultra');
});
