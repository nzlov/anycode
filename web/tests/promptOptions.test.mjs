import assert from 'node:assert/strict';
import { test } from 'node:test';

import * as promptOptions from '../src/components/promptOptions.ts';

const {
  defaultReasoningEffortForModel,
  firstCodexModelValue,
  normalizeCodexModel,
  normalizeCodexSelection,
  normalizeReasoningEffort,
  reasoningEffortOptionsForModel,
} = promptOptions;

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
  assert.equal(normalizeCodexModel(dynamicOptions, ''), 'gpt-5.6-sol');
  assert.deepEqual(
    reasoningEffortOptionsForModel(dynamicOptions, 'gpt-5.6-sol').map(({ label, value }) => ({
      label,
      value,
    })),
    [
      { label: 'low', value: 'low' },
      { label: 'ultra', value: 'ultra' },
    ],
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

test('combined selection preserves a supported model and effort', () => {
  assert.deepEqual(normalizeCodexSelection(dynamicOptions, 'gpt-5.6-sol', 'ultra'), {
    model: 'gpt-5.6-sol',
    effort: 'ultra',
  });
});

test('custom model names preserve the current effort even outside the catalog', () => {
  assert.deepEqual(normalizeCodexSelection(dynamicOptions, ' gpt-6.1-terra ', 'high'), {
    model: 'gpt-6.1-terra',
    effort: 'high',
  });
});

test('custom models without a saved effort use the existing default', () => {
  assert.deepEqual(normalizeCodexSelection(dynamicOptions, 'gpt-6.1-terra', ''), {
    model: 'gpt-6.1-terra',
    effort: 'low',
  });
});
