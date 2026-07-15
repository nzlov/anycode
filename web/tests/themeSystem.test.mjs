import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { extname, join, relative } from 'node:path';
import { test } from 'node:test';

const webRoot = new URL('..', import.meta.url);

function readSource(relativePath) {
  try {
    return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
  } catch {
    return '';
  }
}

function themeVariables(source, selector) {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const block = source.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`, 's'))?.[1] ?? '';
  return new Set([...block.matchAll(/(--ac-[\w-]+)\s*:/g)].map((match) => match[1]));
}

function sourceFiles(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name);
    return entry.isDirectory() ? sourceFiles(path) : [path];
  });
}

const themeSource = readSource('../src/css/theme.scss');
const appStylesSource = readSource('../src/css/app.scss');
const runtimeSource = readSource('../src/theme/tokens.ts');

test('light and dark themes expose the same complete semantic token contract', () => {
  const light = themeVariables(themeSource, ':root');
  const dark = themeVariables(themeSource, '.body--dark');
  const required = [
    '--ac-page',
    '--ac-surface',
    '--ac-surface-muted',
    '--ac-surface-raised',
    '--ac-overlay',
    '--ac-text',
    '--ac-text-muted',
    '--ac-text-inverse',
    '--ac-border',
    '--ac-border-strong',
    '--ac-focus-ring',
    '--ac-status-success-bg',
    '--ac-status-success-text',
    '--ac-status-warning-bg',
    '--ac-status-warning-text',
    '--ac-status-danger-bg',
    '--ac-status-danger-text',
    '--ac-status-info-bg',
    '--ac-status-info-text',
    '--ac-diff-bg',
    '--ac-terminal-bg',
  ];

  assert.ok(light.size > 40, 'theme must define the complete application color roles');
  assert.deepEqual([...dark].sort(), [...light].sort());
  for (const name of required) assert.ok(light.has(name), `${name} is required`);
  assert.match(themeSource, /\.body--dark\s*\{[^}]*color-scheme:\s*dark/s);
});

test('application styles consume the dedicated theme source', () => {
  assert.match(appStylesSource, /^@use ['"]\.\/theme['"];?/);
  assert.doesNotMatch(appStylesSource, /(^|\n)\s*\.body--dark\s*\{/);
  assert.doesNotMatch(appStylesSource, /(^|\n)\s*:root\s*\{[^}]*--ac-(?:surface|text|border)/s);
  assert.match(
    appStylesSource,
    /\.global-settings-nav__active\s*\{[^}]*color:\s*var\(--ac-link\)/s,
  );
});

test('theme runtime only owns mode persistence and Quasar dark switching', () => {
  assert.doesNotMatch(runtimeSource, /setCssVar|themeTokens|#[0-9a-f]{3,8}/i);
  assert.match(runtimeSource, /Dark\.set/);
  assert.match(runtimeSource, /document\.documentElement\.dataset\.themeMode/);
});

test('components do not introduce fixed application colors or light-only palette classes', () => {
  const srcRoot = new URL('../src', import.meta.url).pathname;
  const allowed = new Set([
    'css/quasar.variables.scss',
    'css/theme.scss',
    'components/StaticAnsiOutput.vue',
    'mocks/workbench.ts',
  ]);
  const violations = [];

  for (const path of sourceFiles(srcRoot)) {
    if (!['.scss', '.ts', '.vue'].includes(extname(path))) continue;
    const name = relative(srcRoot, path);
    if (allowed.has(name)) continue;
    const source = readFileSync(path, 'utf8');
    for (const match of source.matchAll(
      /#[0-9a-f]{3,8}\b|\brgba?\s*\([^)]*\)|\b(?:bg-[a-z]+-\d+|text-(?:white|dark|black|grey-\d+))\b|(?:color|text-color|toggle-color)="(?:white|dark|black|blue|grey-\d+|[a-z]+-\d+)"/gi,
    )) {
      violations.push(`${name}:${source.slice(0, match.index).split('\n').length}:${match[0]}`);
    }
  }

  assert.deepEqual(violations, []);
});

test('shared Quasar portal surfaces use semantic theme roles', () => {
  for (const selector of [
    '.q-dialog .q-card',
    '.q-menu',
    '.q-tooltip',
    '.q-notification',
  ]) {
    assert.ok(themeSource.includes(selector), `${selector} theme contract is required`);
  }
  for (const role of ['primary', 'positive', 'warning', 'negative', 'info']) {
    assert.match(
      themeSource,
      new RegExp(`\\.body--dark \\.text-${role}\\s*\\{[^}]*var\\(--ac-`),
      `dark text-${role} must use a high-contrast semantic foreground`,
    );
  }
  for (const selector of ['.q-item--active', '.q-tab--active:not(.question-tab--active)', '.q-field--focused']) {
    assert.ok(themeSource.includes(selector), `${selector} must override Quasar primary in dark mode`);
  }
  assert.match(themeSource, /\.body--dark \.text-blue-grey[^}]*var\(--ac-text-muted\)/s);
});
