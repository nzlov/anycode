import assert from 'node:assert/strict';
import { test } from 'node:test';

import * as promptOptions from '../src/components/promptOptions.ts';

const { defaultReasoningEffortForModel, firstCodexModelValue, reasoningEffortOptionsForModel } =
  promptOptions;

test('model changes use the next model default reasoning effort', () => {
  const nextModel = 'gpt-5.4-mini';
  const nextDefault = reasoningEffortOptionsForModel(nextModel)[0]?.value ?? '';

  assert.equal(defaultReasoningEffortForModel(nextModel), nextDefault);
});

test('model change updates preserve supported models and emit model before effort', () => {
  assert.equal(typeof promptOptions.promptConfigUpdatesForModelChange, 'function');
  if (typeof promptOptions.promptConfigUpdatesForModelChange !== 'function') return;

  const model = 'gpt-5.4-mini';
  assert.deepEqual(promptOptions.promptConfigUpdatesForModelChange(model, 'high'), [
    { field: 'model', value: model },
    { field: 'effort', value: defaultReasoningEffortForModel(model) },
  ]);
});

test('model change updates normalize invalid models and omit unchanged effort', () => {
  assert.equal(typeof promptOptions.promptConfigUpdatesForModelChange, 'function');
  if (typeof promptOptions.promptConfigUpdatesForModelChange !== 'function') return;

  const model = firstCodexModelValue();
  const effort = defaultReasoningEffortForModel(model);
  assert.deepEqual(promptOptions.promptConfigUpdatesForModelChange('unsupported-model', effort), [
    { field: 'model', value: model },
  ]);
});
