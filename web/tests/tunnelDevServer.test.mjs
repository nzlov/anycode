import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const configSource = readFileSync(new URL('../quasar.config.ts', import.meta.url), 'utf8');

test('development server accepts Cloudflare Quick Tunnel hosts without disabling host checks', () => {
  assert.match(configSource, /allowedHosts: \['\.trycloudflare\.com'\]/);
  assert.doesNotMatch(configSource, /allowedHosts:\s*true/);
});
