import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const makefile = readFileSync(new URL('../../Makefile', import.meta.url), 'utf8');
const agentInstructions = readFileSync(new URL('../../AGENTS.md', import.meta.url), 'utf8');

test('make verify is the documented full-project validation entry point', () => {
  assert.match(makefile, /^\.PHONY: verify$/m);
  assert.match(makefile, /^verify:$/m);

  const commands = makefile
    .split('\n')
    .filter((line) => line.startsWith('\t'))
    .map((line) => line.slice(1));

  assert.deepEqual(commands, [
    'go test ./...',
    'go vet ./...',
    'node --test web/tests/*.test.mjs',
    'npm --prefix web run typecheck',
    'npm --prefix web run build',
    'git diff --check',
  ]);
  assert.match(agentInstructions, /常规收口统一运行 `make verify`/);
});
