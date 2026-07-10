import assert from 'node:assert/strict';
import { test } from 'node:test';

import * as promptOptions from '../src/components/promptOptions.ts';

const {
  defaultReasoningEffortForModel,
  firstCodexModelValue,
  normalizeCodexModel,
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

test('model change updates preserve supported models and emit model before effort', () => {
  assert.deepEqual(
    promptOptions.promptConfigUpdatesForModelChange(dynamicOptions, 'gpt-5.6-sol', 'ultra'),
    [
      { field: 'model', value: 'gpt-5.6-sol' },
      { field: 'effort', value: 'low' },
    ],
  );
});

test('model change updates normalize invalid models and omit unchanged effort', () => {
  assert.deepEqual(
    promptOptions.promptConfigUpdatesForModelChange(dynamicOptions, 'unsupported-model', 'low'),
    [{ field: 'model', value: 'gpt-5.6-sol' }],
  );
});
