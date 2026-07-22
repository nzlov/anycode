import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

import {
  auditColorPairs,
  buildContrastReport,
  compositeColors,
  contrastRatio,
  minimumTextContrast,
  parseColor,
  resolveAuditDirectory,
  visibleContrastAuditExpression,
} from '../../scripts/lib/theme-contrast.mjs';
import {
  darkThemeDialogs,
  darkThemeMenus,
  darkThemeRequiredCaptures,
  darkThemeRoutes,
  darkThemeScenarioManifest,
  darkThemeSurfaceStates,
  darkThemeSurfaceViewports,
  darkThemeViewports,
} from '../../scripts/lib/dark-theme-scenarios.mjs';
import { createDarkThemeAudit } from '../../scripts/lib/run-dark-theme-audit.mjs';

const headlessE2ESource = readFileSync(
  new URL('../../scripts/headless-e2e.mjs', import.meta.url),
  'utf8',
);
const quasarConfigSource = readFileSync(new URL('../quasar.config.ts', import.meta.url), 'utf8');
const auditRunnerSource = readFileSync(
  new URL('../../scripts/lib/run-dark-theme-audit.mjs', import.meta.url),
  'utf8',
);

test('WCAG color parsing, alpha composition and contrast use the standard formula', () => {
  assert.deepEqual(parseColor('#fff'), { r: 255, g: 255, b: 255, a: 1 });
  assert.deepEqual(parseColor('rgb(0 0 0 / 50%)'), { r: 0, g: 0, b: 0, a: 0.5 });
  const composite = compositeColors('rgb(0 0 0 / 50%)', '#fff');
  assert.ok(Math.abs(composite.r - 127.5) < 0.01);
  assert.equal(contrastRatio('#000', '#fff'), 21);
  assert.ok(Math.abs(contrastRatio('#777', '#fff') - 4.478) < 0.01);
});

test('text thresholds distinguish normal and large bold text', () => {
  assert.equal(minimumTextContrast({ fontSize: '16px', fontWeight: '400' }), 4.5);
  assert.equal(minimumTextContrast({ fontSize: '24px', fontWeight: '400' }), 3);
  assert.equal(minimumTextContrast({ fontSize: '18.66px', fontWeight: '700' }), 3);
});

test('pair audit and report preserve violations and manual review warnings', () => {
  const [entry] = auditColorPairs([
    { id: 'muted', foreground: '#777', background: '#fff', minimum: 4.5 },
  ]);
  assert.equal(entry.passed, false);
  const report = buildContrastReport({
    runId: 'run-1',
    scenarios: ['overview'],
    entries: [
      { status: 'passed' },
      { status: 'violation', selector: '.bad' },
      { status: 'manual-review', selector: '.media' },
    ],
  });
  assert.deepEqual(report.summary, { checked: 1, violations: 1, warnings: 1 });
  assert.equal(report.violations[0].selector, '.bad');
  assert.equal(report.warnings[0].selector, '.media');
});

test('artifact paths stay under ANYCODE_ARTIFACT_DIR', () => {
  assert.equal(
    resolveAuditDirectory('/tmp/card-artifacts', 'run-1'),
    '/tmp/card-artifacts/dark-theme-audit/run-1',
  );
  assert.throws(() => resolveAuditDirectory('', 'run-1'), /ANYCODE_ARTIFACT_DIR/);
  assert.throws(() => resolveAuditDirectory('/tmp/card-artifacts', '../escape'), /Invalid/);
});

test('scenario manifest fixes the approved 8 route, 14 dialog, 6 menu and 3 viewport scope', () => {
  assert.equal(darkThemeRoutes.length, 8);
  assert.equal(darkThemeDialogs.length, 14);
  assert.equal(darkThemeMenus.length, 6);
  assert.deepEqual(
    darkThemeViewports.map(({ width, height }) => `${width}x${height}`),
    ['1440x900', '900x900', '390x844'],
  );
  assert.deepEqual(darkThemeScenarioManifest().dynamicOverlays, [
    'select-popup',
    'tooltip',
    'notification',
  ]);
  assert.deepEqual(darkThemeSurfaceStates, {
    'route-diff': ['all', 'single'],
    'dialog-global-settings': ['projects', 'quick-commands'],
    'dialog-questions': ['questions', 'diff'],
    'dialog-forward-approval': ['result', 'diff'],
  });
  assert.deepEqual(darkThemeSurfaceViewports, { 'menu-prompt-config': ['tablet', 'mobile'] });
  assert.equal(darkThemeRequiredCaptures().length, 104);
  const expression = visibleContrastAuditExpression();
  assert.match(expression, /getComputedStyle/);
  assert.match(expression, /composite\(foreground, background\.color\)/);
  assert.match(expression, /:disabled/);
  assert.match(expression, /element\.value \|\| element\.placeholder/);
  assert.match(
    auditRunnerSource,
    /const root = roots\.at\(-1\) \|\| document/,
  );
  assert.match(auditRunnerSource, /Duplicate dark theme capture/);
});

test('audit runner requires the artifact root and exposes all-viewport capture', () => {
  assert.throws(
    () => createDarkThemeAudit({ artifactDir: '', runId: 'run-1', driver: {} }),
    /ANYCODE_ARTIFACT_DIR/,
  );
  const audit = createDarkThemeAudit({
    artifactDir: '/tmp/card-artifacts',
    runId: 'run-1',
    driver: {},
  });
  assert.equal(typeof audit.captureAllViewports, 'function');
  assert.equal(audit.outputDir, '/tmp/card-artifacts/dark-theme-audit/run-1');
});

test('self-contained headless E2E invokes every approved route, dialog and menu audit id', () => {
  assert.match(headlessE2ESource, /--dark-theme-audit/);
  assert.match(headlessE2ESource, /darkThemeAudit\.finish\(\)/);
  for (const route of darkThemeRoutes) {
    assert.ok(
      headlessE2ESource.includes('`route-${route.id}`'),
      `route-${route.id} audit call is required`,
    );
  }
  for (const dialog of darkThemeDialogs) {
    assert.ok(
      headlessE2ESource.includes(`'dialog-${dialog}'`),
      `dialog-${dialog} audit call is required`,
    );
  }
  for (const menu of darkThemeMenus) {
    assert.ok(
      headlessE2ESource.includes(`'menu-${menu}'`),
      `menu-${menu} audit call is required`,
    );
  }
  for (const overlay of ['select-popup', 'tooltip', 'notification']) {
    assert.ok(
      headlessE2ESource.includes(`'overlay-${overlay}'`),
      `overlay-${overlay} audit call is required`,
    );
  }
  assert.equal(
    [...headlessE2ESource.matchAll(/auditDarkThemeSurface\('menu-header-more'\)/g)].length,
    1,
  );
  for (const [surfaceId, states] of Object.entries(darkThemeSurfaceStates)) {
    for (const stateId of states) {
      assert.ok(
        headlessE2ESource.includes(`'${surfaceId}', '${stateId}'`),
        `${surfaceId}:${stateId} audit state is required`,
      );
    }
  }
  assert.match(headlessE2ESource, /query SessionTranscript\(\$input: ListTranscriptEventsInput!\)/);
  assert.doesNotMatch(headlessE2ESource, /query SessionEvents/);
  assert.match(quasarConfigSource, /plugins:\s*\[[^\]]*['"]Dialog['"][^\]]*\]/);
});
