import { mkdir, writeFile } from 'node:fs/promises';

import {
  buildContrastReport,
  resolveAuditDirectory,
  visibleContrastAuditExpression,
} from './theme-contrast.mjs';
import {
  darkThemeDialogs,
  darkThemeMenus,
  darkThemeRequiredCaptures,
  darkThemeRoutes,
  darkThemeScenarioManifest,
  darkThemeSurfaceStates,
  darkThemeViewports,
} from './dark-theme-scenarios.mjs';

const requiredSurfaceIds = new Set([
  ...darkThemeRoutes.map((route) => `route-${route.id}`),
  ...darkThemeDialogs.map((dialog) => `dialog-${dialog}`),
  ...darkThemeMenus.map((menu) => `menu-${menu}`),
  'overlay-select-popup',
  'overlay-tooltip',
  'overlay-notification',
]);

function captureKey(surfaceId, stateId, viewport) {
  return `${surfaceId}:${stateId}:${viewport}`;
}

export function createDarkThemeAudit({ artifactDir, runId, driver }) {
  const outputDir = resolveAuditDirectory(artifactDir, runId);
  const captures = [];
  const capturedKeys = new Set();
  const entries = [];

  async function prepare() {
    await mkdir(outputDir, { recursive: true });
    await driver.evaluate(`localStorage.setItem('anycode.theme.mode', 'dark')`);
  }

  async function scanVisible(surfaceId, stateId, viewport, overlay = '') {
    const scanned = await driver.evaluate(visibleContrastAuditExpression());
    for (const entry of scanned) {
      entries.push({ ...entry, surfaceId, stateId, viewport: viewport.id, ...(overlay ? { overlay } : {}) });
    }
  }

  async function sweepDynamicOverlays(surfaceId, stateId, viewport) {
    const selectIds = await driver.evaluate(`(() => {
      const visible = (element) => {
        const rect = element.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 &&
          rect.left < innerWidth && rect.top < innerHeight;
      };
      const surfaceId = ${JSON.stringify(surfaceId)};
      const roots = surfaceId.startsWith('dialog-')
        ? Array.from(document.querySelectorAll('.q-dialog')).filter(visible)
        : surfaceId.startsWith('menu-')
          ? Array.from(document.querySelectorAll('.q-menu')).filter(visible)
          : [];
      const root = roots.at(-1) || document;
      let index = 0;
      return Array.from(root.querySelectorAll('.q-select')).filter((element) =>
        visible(element) && !element.classList.contains('disabled')
      ).map((element) => {
        const id = 'theme-audit-select-' + index++;
        element.dataset.themeAuditId = id;
        return id;
      });
    })()`);
    let selects = 0;
    for (const id of selectIds) {
      await driver.evaluate(`document.querySelector('[data-theme-audit-id=${JSON.stringify(id)}]')?.click()`);
      await driver.sleep(150);
      const opened = await driver.evaluate(`Array.from(document.querySelectorAll('.q-menu')).some((element) => {
        const rect = element.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0;
      })`);
      if (!opened) {
        entries.push({ status: 'violation', surfaceId, stateId, viewport: viewport.id, overlay: 'select', selector: id, reason: 'popup-not-opened' });
      } else {
        selects += 1;
        await scanVisible(surfaceId, stateId, viewport, `select:${id}`);
      }
      await driver.pressEscape();
      await driver.sleep(100);
    }

    const tooltipTargets = await driver.evaluate(`(() => {
      const visible = (element) => {
        const rect = element.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 &&
          rect.left < innerWidth && rect.top < innerHeight;
      };
      const surfaceId = ${JSON.stringify(surfaceId)};
      const roots = surfaceId.startsWith('dialog-')
        ? Array.from(document.querySelectorAll('.q-dialog')).filter(visible)
        : surfaceId.startsWith('menu-')
          ? Array.from(document.querySelectorAll('.q-menu')).filter(visible)
          : [];
      const root = roots.at(-1) || document;
      let index = 0;
      return Array.from(root.querySelectorAll('button[aria-label], [role="button"][aria-label]')).filter((element) =>
        visible(element) && !element.matches(':disabled, [aria-disabled="true"]')
      ).map((element) => {
        const rect = element.getBoundingClientRect();
        const id = 'theme-audit-tooltip-' + index++;
        element.dataset.themeAuditTooltipId = id;
        return { id, x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 };
      });
    })()`);
    let tooltips = 0;
    for (const target of tooltipTargets) {
      await driver.moveMouse(target.x, target.y);
      await driver.sleep(180);
      const opened = await driver.evaluate(`Array.from(document.querySelectorAll('.q-tooltip')).some((element) => {
        const rect = element.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0;
      })`);
      if (opened) {
        tooltips += 1;
        await scanVisible(surfaceId, stateId, viewport, `tooltip:${target.id}`);
      }
    }
    await driver.moveMouse(0, 0);
    return { selects, tooltipTargets: tooltipTargets.length, tooltips };
  }

  async function capture(surfaceId, viewport, stateId = 'default') {
    if (!requiredSurfaceIds.has(surfaceId) && !surfaceId.startsWith('overlay-')) {
      throw new Error(`Unknown dark theme surface: ${surfaceId}`);
    }
    const allowedStates = darkThemeSurfaceStates[surfaceId] || ['default'];
    if (!allowedStates.includes(stateId)) {
      throw new Error(`Unknown dark theme state: ${surfaceId}:${stateId}`);
    }
    const key = captureKey(surfaceId, stateId, viewport.id);
    if (capturedKeys.has(key)) {
      throw new Error(`Duplicate dark theme capture: ${key}`);
    }
    await driver.setViewport(viewport.width, viewport.height);
    await driver.waitForStableUI();
    const theme = await driver.evaluate(`(() => ({
      mode: document.documentElement.dataset.themeMode,
      dark: document.body.classList.contains('body--dark'),
      colorScheme: getComputedStyle(document.body).colorScheme,
      overflow: document.documentElement.scrollWidth > innerWidth + 1,
      dialogVisible: Array.from(document.querySelectorAll('.q-dialog')).some((element) => {
        const rect = element.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0;
      }),
      menuVisible: Array.from(document.querySelectorAll('.q-menu')).some((element) => {
        const rect = element.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0;
      }),
      tooltipVisible: Array.from(document.querySelectorAll('.q-tooltip')).some((element) => {
        const rect = element.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0;
      }),
      notificationVisible: Array.from(document.querySelectorAll('.q-notification')).some((element) => {
        const rect = element.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0;
      }),
    }))()`);
    if (theme.mode !== 'dark' || !theme.dark || theme.colorScheme !== 'dark') {
      throw new Error(`${surfaceId} did not apply dark theme: ${JSON.stringify(theme)}`);
    }
    if (surfaceId.startsWith('dialog-') && !theme.dialogVisible) {
      throw new Error(`${surfaceId} is not visible at ${viewport.id}`);
    }
    if (surfaceId.startsWith('menu-') && !theme.menuVisible) {
      throw new Error(`${surfaceId} is not visible at ${viewport.id}`);
    }
    if (surfaceId === 'overlay-select-popup' && !theme.menuVisible) {
      throw new Error(`${surfaceId} is not visible at ${viewport.id}`);
    }
    if (surfaceId === 'overlay-tooltip' && !theme.tooltipVisible) {
      throw new Error(`${surfaceId} is not visible at ${viewport.id}`);
    }
    if (surfaceId === 'overlay-notification' && !theme.notificationVisible) {
      throw new Error(`${surfaceId} is not visible at ${viewport.id}`);
    }
    await scanVisible(surfaceId, stateId, viewport);
    if (theme.overflow) {
      entries.push({ status: 'violation', surfaceId, stateId, viewport: viewport.id, reason: 'horizontal-overflow' });
    }
    const screenshot = `${viewport.id}--${surfaceId}${stateId === 'default' ? '' : `--${stateId}`}.png`;
    await driver.screenshot(screenshot, outputDir);
    const overlays = surfaceId.startsWith('overlay-')
      ? { selects: 0, tooltipTargets: 0, tooltips: 0 }
      : await sweepDynamicOverlays(surfaceId, stateId, viewport);
    captures.push({ surfaceId, stateId, viewport: viewport.id, screenshot, overlays });
    capturedKeys.add(key);
  }

  async function captureAllViewports(surfaceId, stateId = 'default') {
    for (const viewport of darkThemeViewports) await capture(surfaceId, viewport, stateId);
  }

  async function finish() {
    const captured = new Set(captures.map((capture) => captureKey(capture.surfaceId, capture.stateId, capture.viewport)));
    const missing = darkThemeRequiredCaptures().filter(
      (capture) => !captured.has(captureKey(capture.surfaceId, capture.stateId, capture.viewport)),
    );
    const manifest = {
      ...darkThemeScenarioManifest(),
      runId,
      captures,
      missing,
    };
    const report = buildContrastReport({
      runId,
      entries,
      scenarios: captures.map(({ surfaceId, stateId, viewport }) => `${viewport}:${surfaceId}:${stateId}`),
    });
    await writeFile(`${outputDir}/manifest.json`, `${JSON.stringify(manifest, null, 2)}\n`);
    await writeFile(`${outputDir}/contrast-report.json`, `${JSON.stringify(report, null, 2)}\n`);
    if (missing.length || report.summary.violations || report.summary.warnings) {
      throw new Error(
        `Dark theme audit failed: ${missing.length} missing, ${report.summary.violations} violations, ${report.summary.warnings} manual reviews. Report: ${outputDir}`,
      );
    }
    return { outputDir, manifest, report };
  }

  return { prepare, capture, captureAllViewports, finish, outputDir };
}
