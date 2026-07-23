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
const dailyBackgroundSource = readSource('../src/theme/dailyBackground.ts');
const dailyBackgroundModelSource = readSource('../src/theme/dailyBackgroundModel.js');
const appearanceRuntimeSource = readSource('../src/theme/appearance.ts');
const solidThemesSource = readSource('../src/theme/solidThemes.ts');
const bootSource = readSource('../src/boot/theme.ts');
const appearanceSettingsSource = readSource('../src/services/appearanceSettings.ts');
const globalSettingsSource = readSource('../src/components/GlobalSettingsDialog.vue');
const appSource = readSource('../src/App.vue');
const loginSource = readSource('../src/pages/LoginPage.vue');
const notFoundSource = readSource('../src/pages/ErrorNotFound.vue');

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
  assert.match(themeSource, /:root\s*\{[^}]*--ac-background-mask-color:\s*#ffffff/s);
  assert.match(themeSource, /\.body--dark\s*\{[^}]*--ac-background-mask-color:\s*#000000/s);
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

test('native scrollbars follow the global Quasar-style theme treatment', () => {
  assert.match(appStylesSource, /\*\s*\{[^}]*scrollbar-width:\s*thin/s);
  assert.match(appStylesSource, /\*\s*\{[^}]*scrollbar-color:[^}]*var\(--ac-text\)/s);
  assert.match(
    appStylesSource,
    /\*::\-webkit-scrollbar\s*\{[^}]*width:\s*10px[^}]*height:\s*10px/s,
  );
  assert.match(appStylesSource, /\*::\-webkit-scrollbar-thumb:hover\s*\{/);
  assert.match(appStylesSource, /\*::\-webkit-scrollbar-thumb:active\s*\{/);
});

test('theme runtime only owns mode persistence and Quasar dark switching', () => {
  assert.doesNotMatch(runtimeSource, /setCssVar|themeTokens|#[0-9a-f]{3,8}/i);
  assert.match(runtimeSource, /Dark\.set/);
  assert.match(runtimeSource, /document\.documentElement\.dataset\.themeMode/);
});

test('daily background maps official M3 roles through the theme boundary', () => {
  for (const mode of ['light', 'dark']) {
    for (const role of [
      'background',
      'surface-container-low',
      'on-surface',
      'outline',
      'primary',
      'primary-fixed',
      'secondary-fixed',
      'tertiary-fixed',
      'error',
    ]) {
      assert.ok(themeSource.includes(`--ac-m3-${mode}-${role}`), `${mode} ${role} is required`);
    }
  }
  assert.match(themeSource, /--q-primary:\s*var\(--ac-action-primary-bg\)/);
  assert.match(bootSource, /initializeDailyBackground\(\)/);
  assert.match(bootSource, /getAppearanceSettings/);
  assert.match(bootSource, /applyAppearanceSettings/);
  assert.match(dailyBackgroundSource, /crypto\.subtle\.digest\('SHA-256'/);
  assert.match(dailyBackgroundSource, /cache:\s*'no-store'/);
  assert.match(dailyBackgroundSource, /cache:\s*'no-cache'/);
  assert.match(dailyBackgroundSource, /createImageBitmap/);
  assert.match(dailyBackgroundSource, /resolveMaterialPaletteCache/);
  assert.match(dailyBackgroundModelSource, /QuantizerCelebi\.quantize\(pixels, 128\)/);
  assert.match(dailyBackgroundModelSource, /Score\.score\(quantized\)/);
  for (const scheme of [
    'SchemeContent',
    'SchemeFidelity',
    'SchemeTonalSpot',
    'SchemeVibrant',
    'SchemeExpressive',
    'SchemeRainbow',
    'SchemeFruitSalad',
    'SchemeNeutral',
    'SchemeMonochrome',
  ]) {
    assert.ok(dailyBackgroundModelSource.includes(scheme), `${scheme} is required`);
  }
  assert.doesNotMatch(dailyBackgroundSource, /indexedDB|caches\.open|CacheStorage/);
});

test('Quasar and application status colors are aliases of M3 dynamic roles', () => {
  const quasarRoles = {
    primary: 'action-primary-bg',
    secondary: 'secondary',
    accent: 'tertiary',
    positive: 'primary',
    negative: 'error',
    info: 'secondary',
    warning: 'tertiary',
    dark: 'surface',
    'dark-page': 'page',
  };

  for (const [quasarRole, appRole] of Object.entries(quasarRoles)) {
    assert.equal(
      themeSource.match(new RegExp(`--q-${quasarRole}:\\s*var\\(--ac-${appRole}\\)`, 'g'))?.length,
      2,
      `${quasarRole} must follow the light and dark M3 palettes`,
    );
  }

  const statusRoles = {
    'on-warning': 'on-tertiary',
    'status-neutral-bg': 'surface-muted',
    'status-neutral-text': 'text',
    'status-neutral-border': 'border-strong',
    'status-success-bg': 'primary-container',
    'status-success-text': 'on-primary-container',
    'status-success-border': 'primary',
    'status-warning-bg': 'tertiary-container',
    'status-warning-text': 'on-tertiary-container',
    'status-warning-border': 'tertiary',
    'status-danger-bg': 'error-container',
    'status-danger-text': 'on-error-container',
    'status-danger-border': 'error',
    'status-info-bg': 'secondary-container',
    'status-info-text': 'on-secondary-container',
    'status-info-border': 'secondary',
    'status-mode-bg': 'secondary-container',
    'status-mode-text': 'on-secondary-container',
    'status-mode-border': 'secondary',
  };

  for (const [statusRole, appRole] of Object.entries(statusRoles)) {
    assert.equal(
      themeSource.match(new RegExp(`--ac-${statusRole}:\\s*var\\(--ac-${appRole}\\)`, 'g'))?.length,
      2,
      `${statusRole} must follow the light and dark M3 palettes`,
    );
  }
  assert.doesNotMatch(themeSource, /--ac-(?:on-warning|status-[\w-]+):\s*#/);
});

test('shared Quasar page surfaces use the M3 theme boundary', () => {
  assert.match(
    themeSource,
    /\.q-card,\s*\.q-table__card\s*\{[^}]*color:\s*var\(--ac-text\)[^}]*background:\s*var\(--ac-surface\)/s,
  );
  assert.match(themeSource, /\.q-card--bordered[^}]*border-color:\s*var\(--ac-border\)/s);
  assert.match(
    themeSource,
    /\.q-tab-panels\s*\{[^}]*color:\s*inherit[^}]*background:\s*transparent/s,
  );
  assert.match(themeSource, /\.q-list--bordered[^}]*border-color:\s*var\(--ac-border\)/s);
  assert.match(themeSource, /\.q-separator\s*\{[^}]*background:\s*var\(--ac-border\)/s);
  assert.match(
    appStylesSource,
    /\.session-table\s*\{[^}]*color:\s*var\(--ac-text\)[^}]*background:\s*var\(--ac-surface\)/s,
  );
  assert.match(
    appStylesSource,
    /\.session-table thead tr,[^}]*\.q-table__bottom\s*\{[^}]*background:\s*var\(--ac-surface-muted\)/s,
  );
});

test('global appearance persists all background modes and applies them immediately', () => {
  assert.match(globalSettingsSource, /name="appearance"[^>]*icon="palette"/);
  assert.match(globalSettingsSource, /backgroundTypeOptions/);
  assert.match(globalSettingsSource, /solidThemeOptions/);
  assert.match(globalSettingsSource, /uploadAppearanceWallpaper/);
  assert.match(globalSettingsSource, /背景遮罩/);
  assert.match(globalSettingsSource, /壁纸选色算法/);
  assert.match(globalSettingsSource, /wallpaperColorSchemeOptions/);
  assert.match(globalSettingsSource, /applyAppearanceSettings\(settings\)/);
  assert.match(appearanceSettingsSource, /query AppearanceSettings/);
  assert.match(appearanceSettingsSource, /mutation UpdateAppearanceSettings/);
  assert.match(appearanceSettingsSource, /mutation UploadAppearanceWallpaper/);
  assert.equal(appearanceSettingsSource.match(/\{ label: '[^']+', value: '[^']+' \}/g)?.length, 9);
  assert.equal(
    solidThemesSource.match(/label: '[^']+', value: '[^']+', color: '#[0-9a-f]+'/g)?.length,
    8,
  );
  assert.match(appearanceRuntimeSource, /activateSolidBackground\(theme\.color\)/);
  assert.match(appearanceRuntimeSource, /activateUploadedBackground/);
  assert.match(appearanceRuntimeSource, /activateBingBackground/);
  assert.match(
    dailyBackgroundSource,
    /activateSolidBackground[\s\S]*createMaterialPalettes\(sourceColor, 'tonal_spot'\)/,
  );
  assert.match(dailyBackgroundSource, /activateUploadedBackground[\s\S]*extractImageSourceColor/);
});

test('root exposes the shared background without an image caption', () => {
  assert.doesNotMatch(appSource, /app-daily-credit|dailyBackgroundState/);
  assert.doesNotMatch(appStylesSource, /\.app-daily-credit/);
  assert.match(
    appStylesSource,
    /#q-app::before[^}]*background-image:\s*var\(--ac-background-image/s,
  );
  assert.match(appStylesSource, /html\[data-background='ready'\]/);
  assert.match(appStylesSource, /#q-app::after[^}]*--ac-background-mask-opacity/s);
  assert.doesNotMatch(appStylesSource, /--ac-daily-veil/);
  assert.doesNotMatch(themeSource, /--ac-daily-veil/);
  assert.doesNotMatch(appStylesSource, /var\(--ac-page\) 16%, transparent/);
  assert.equal(
    appStylesSource.match(/--ac-page-layer:\s*transparent/g)?.length,
    2,
    'light and dark modes remove the page layer over the background',
  );
  assert.match(appStylesSource, /\.app-layout\s*\{[^}]*background:\s*var\(--ac-page-layer\)/s);
  assert.doesNotMatch(appStylesSource, /\.app-layout\s*,\s*\.workbench-page\s*\{/s);
  assert.match(loginSource, /background:\s*var\(--ac-page-layer\)/);
  assert.match(notFoundSource, /background:\s*var\(--ac-page-layer\)/);
  assert.match(notFoundSource, /error-not-found-page__content/);
  assert.match(notFoundSource, /error-not-found-page__code/);
  assert.match(notFoundSource, /error-not-found-page__message/);
  assert.match(notFoundSource, /color:\s*var\(--ac-text-muted\)/);
  assert.match(notFoundSource, /@media \(max-width: 699px\)/);
  assert.doesNotMatch(notFoundSource, /style=/);
});

test('components do not introduce fixed application colors or light-only palette classes', () => {
  const srcRoot = new URL('../src', import.meta.url).pathname;
  const allowed = new Set([
    'css/quasar.variables.scss',
    'css/theme.scss',
    'components/StaticAnsiOutput.vue',
    'mocks/workbench.ts',
    'theme/solidThemes.ts',
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
  for (const selector of ['.q-dialog .q-card', '.q-menu', '.q-tooltip', '.q-notification']) {
    assert.ok(themeSource.includes(selector), `${selector} theme contract is required`);
  }
  for (const role of ['primary', 'positive', 'warning', 'negative', 'info']) {
    assert.match(
      themeSource,
      new RegExp(`\\.body--dark \\.text-${role}\\s*\\{[^}]*var\\(--ac-`),
      `dark text-${role} must use a high-contrast semantic foreground`,
    );
  }
  for (const selector of [
    '.q-item--active',
    '.q-tab--active:not(.question-tab--active)',
    '.q-field--focused',
  ]) {
    assert.ok(
      themeSource.includes(selector),
      `${selector} must override Quasar primary in dark mode`,
    );
  }
  assert.match(themeSource, /\.body--dark \.text-blue-grey[^}]*var\(--ac-text-muted\)/s);
});
