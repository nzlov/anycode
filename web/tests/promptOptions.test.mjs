import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  defaultReasoningEffortForModel,
  reasoningEffortOptionsForModel,
} from '../src/components/promptOptions.ts';

test('model changes use the next model default reasoning effort', () => {
  const nextModel = 'gpt-5.4-mini';
  const nextDefault = reasoningEffortOptionsForModel(nextModel)[0]?.value ?? '';

  assert.equal(defaultReasoningEffortForModel(nextModel), nextDefault);
});
