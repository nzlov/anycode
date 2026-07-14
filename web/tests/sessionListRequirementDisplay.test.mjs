import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

test('session table renders the requirement once as the detail link', () => {
  const source = readSource('../src/pages/SessionsPage.vue');

  assert.match(source, /\{\{ props\.row\.title \}\}/);
  assert.doesNotMatch(source, /props\.row\.summary/);
});

test('session card summary remains available without being rendered in the table', () => {
  const source = readSource('../src/services/sessions.ts');
  const schema = readSource('../../internal/interfaces/graphql/graph/schema.graphqls');

  assert.match(source, /summary: string;/);
  assert.match(source, /requirementSummary/);
  assert.match(schema, /requirementSummary: String!/);
});
