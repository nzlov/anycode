#!/usr/bin/env node
import { spawn } from 'node:child_process';
import { mkdir, rm, writeFile } from 'node:fs/promises';
import { homedir } from 'node:os';
import { setTimeout as sleep } from 'node:timers/promises';

import { createDarkThemeAudit } from './lib/run-dark-theme-audit.mjs';
import { darkThemeRoutes, darkThemeViewports } from './lib/dark-theme-scenarios.mjs';

const args = new Set(process.argv.slice(2));
const manageDocker = args.has('--manage-docker');
const darkThemeAuditEnabled = args.has('--dark-theme-audit');
const accessKey = process.env.ANYCODE_ACCESS_KEY || 'test';
const baseURL = process.env.ANYCODE_E2E_BASE_URL || 'http://127.0.0.1:8080';
const codexHome = process.env.ANYCODE_CODEX_HOME || `${homedir()}/.codex`;
const stamp = `${Date.now()}-${Math.random().toString(16).slice(2, 8)}`;
const plainPath = process.env.ANYCODE_E2E_PLAIN_PATH || `/workspaces/e2e-anycode-plain-${stamp}`;
const gitPath = process.env.ANYCODE_E2E_GIT_PATH || `/workspaces/e2e-anycode-git-${stamp}`;
const debugPort = Number(process.env.ANYCODE_E2E_CDP_PORT || 9333);
const debugURL = `http://127.0.0.1:${debugPort}`;
const userDataDir = `/tmp/anycode-chromium-e2e-${stamp}`;
const artifactDir = process.env.ANYCODE_ARTIFACT_DIR || '';
const e2eDataDir = process.env.ANYCODE_E2E_DATA_DIR || '';
const screenshotDir =
  process.env.ANYCODE_E2E_SCREENSHOT_DIR ||
  (artifactDir ? `${artifactDir}/headless-e2e/${stamp}` : '/tmp/anycode-headless');
const overrideFile = `/tmp/anycode-codex-override-${stamp}.yml`;

let chrome;
let page;
let browserFailures;
let darkThemeAudit;

try {
  if (manageDocker) {
    await startDocker();
    await prepareContainerWorkspaces();
  }

  await waitForHTTPHealth();
  await rm(userDataDir, { recursive: true, force: true }).catch(() => {});
  await rm(screenshotDir, { recursive: true, force: true });
  await mkdir(screenshotDir, { recursive: true });

  chrome = launchChromium();
  await waitForChrome();
  page = await connectPage();

  const browserState = trackBrowserFailures(page);
  browserFailures = browserState;
  await page.send('Page.enable');
  await page.send('DOM.enable');
  await page.send('Runtime.enable');
  await page.send('Log.enable');
  await page.send('Network.enable');
  await page.send('Page.addScriptToEvaluateOnNewDocument', {
    source: [
      `localStorage.setItem('anycode.accessKey', ${JSON.stringify(accessKey)});`,
      darkThemeAuditEnabled ? `localStorage.setItem('anycode.theme.mode', 'dark');` : '',
    ].join('\n'),
  });

  await setViewport(1440, 900);
  await navigate('/');
  await evaluate(`localStorage.setItem('anycode.accessKey', ${JSON.stringify(accessKey)});`);
  await navigate('/');
  if (darkThemeAuditEnabled) {
    darkThemeAudit = createDarkThemeAudit({
      artifactDir,
      runId: stamp,
      driver: {
        evaluate,
        setViewport,
        waitForStableUI: () => sleep(500),
        screenshot,
        sleep,
        moveMouse: (x, y) =>
          page.send('Input.dispatchMouseEvent', { type: 'mouseMoved', x, y }),
        pressEscape: async () => {
          await page.send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Escape', code: 'Escape' });
          await page.send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Escape', code: 'Escape' });
        },
      },
    });
    await darkThemeAudit.prepare();
  }
  browserState.clear();
  await waitForText('暂无卡片');
  await assertNoHorizontalOverflow('desktop overview');
  await screenshot('01-overview-desktop.png');

  await assertDirectoryBrowser();
  await screenshotDirectoryDialog();
  const plainProject = await ensureProject({
    path: plainPath,
    name: `E2E Plain ${stamp}`,
    isGit: false,
  });
  const gitProject = await ensureProject({
    path: gitPath,
    name: `E2E Git ${stamp}`,
    isGit: true,
  });
  assert(gitProject.gitState.branches.some((branch) => branch.name === 'main'), 'git project missing main branch');

  const plainSession = await runPlainSession(plainProject.id);
  const gitSession = await runGitSession(gitProject.id);

  await navigate('/');
  await waitForText('ANYCODE_GIT_E2E_OK');
  await assertSessionCardShowsProject('ANYCODE_GIT_E2E_OK', gitProject.name);
  await assertProjectChipPersistence(gitProject.id, gitProject.name, 'ANYCODE_GIT_E2E_OK');
  await assertNoHorizontalOverflow('overview branch lanes');
  await screenshot('02-overview-branch-lanes.png');

  await assertNewSessionDefaultProject({
    route: `/#/?projectId=${gitProject.id}`,
    expectedProjectName: gitProject.name,
    label: 'project route default project',
    screenshotName: '11-new-session-dialog.png',
    withAttachments: true,
  });
  await evaluate(`localStorage.setItem('anycode.lastNewSessionProjectId', ${JSON.stringify(plainProject.id)});`);
  await assertNewSessionDefaultProject({
    route: '/',
    expectedProjectName: plainProject.name,
    label: 'overview last project default',
  });

  await assertWorkflowConfigInteractions(gitProject.id);
  const approvalSession = await assertWorkflowCardFlow(gitProject.id);
  const questionSession = await assertAnswerUserFlow(plainProject.id);

  await navigate('/#/sessions');
  await waitForText('会话表格');
  await waitForText('请只输出 OK');
  await waitForText('ANYCODE_GIT_E2E_OK');
  await assertSessionsTableProjectName('ANYCODE_GIT_E2E_OK', gitProject.name, gitProject.id);
  await assertNoHorizontalOverflow('desktop sessions');
  await screenshot('03-sessions-desktop.png');

  await assertSessionDetailResizableLayout(plainSession.id, gitSession.id);

  await navigate(`/#/sessions/${plainSession.id}`);
  await waitForText('请只输出 OK');
  await waitForText('会话信息');
  await assertSessionDetailReadableLayout('plain session detail layout');
  await assertNoHorizontalOverflow('plain session detail');
  await screenshot('04-plain-session-detail.png');

  await navigate(`/#/sessions/${gitSession.id}`);
  await waitForText('当前变更');
  await clickText('当前变更');
  await waitForText('e2e-codex-output.txt');
  await screenshotSessionDiffWorkspace('e2e-codex-output.txt');
  await assertSessionDetailReadableLayout('git session detail layout');
  await assertNoHorizontalOverflow('git session detail');
  await screenshot('05-git-session-detail.png');

  await appendPrompt(gitSession.id);

  await navigate(`/#/diff?sessionId=${gitSession.id}&mode=all`);
  await waitForText('Diff');
  await waitForText('e2e-codex-output.txt');
  await assertNoHorizontalOverflow('git diff');
  await screenshot('06-git-diff-desktop.png');

  await navigate(`/#/projects/${gitProject.id}/workflow`);
  await waitForText('流程配置');
  await assertNoHorizontalOverflow('workflow');
  await screenshot('07-workflow-desktop.png');

  await setViewport(390, 844);
  await navigate('/');
  await waitForVisibleSelector('.overview-filter-toolbar');
  await assertNoHorizontalOverflow('mobile overview');
  await screenshot('08-overview-mobile.png');

  await navigate(`/#/sessions/${gitSession.id}`);
  await waitForVisibleSelector('.stream-card__body .event-list__item');
  await assertSessionDetailReadableLayout('mobile git session detail layout');
  await assertNoHorizontalOverflow('mobile git session detail');
  await screenshot('09-git-session-mobile.png');

  if (darkThemeAudit) {
    await auditDarkThemeRoutes({ gitProject, gitSession });
    await auditDarkThemeDialogsAndMenus({ gitProject, gitSession, approvalSession });
    await darkThemeAudit.finish();
  }

  browserState.assertClean();
  console.log(JSON.stringify({
    ok: true,
    plainProjectId: plainProject.id,
    plainSessionId: plainSession.id,
    plainFinalStatus: plainSession.finalStatus,
    gitProjectId: gitProject.id,
    gitSessionId: gitSession.id,
    gitFinalStatus: gitSession.finalStatus,
    approvalSessionId: approvalSession.id,
    approvalStatus: approvalSession.status,
    questionSessionId: questionSession.id,
    questionBatchId: questionSession.batchId,
    gitDiffFile: 'e2e-codex-output.txt',
    screenshots: [
      '01-overview-desktop.png',
      '02-overview-branch-lanes.png',
      '03-sessions-desktop.png',
      '04-plain-session-detail.png',
      '05-git-session-detail.png',
      '06-git-diff-desktop.png',
      '07-workflow-desktop.png',
      '08-overview-mobile.png',
      '09-git-session-mobile.png',
      '10-directory-dialog.png',
      '11-new-session-dialog.png',
      '12-answer-user-dialog.png',
      '13-session-diff-workspace.png',
      '14-session-splitter-default.png',
      '15-session-splitter-persisted.png',
      '16-session-splitter-clamped.png',
      '17-session-splitter-restored.png',
      '18-session-splitter-mobile.png',
    ].map((name) => `${screenshotDir}/${name}`),
  }, null, 2));
} catch (error) {
  console.error(error?.stack || error);
  process.exitCode = 1;
} finally {
  if (page) page.close();
  await stopChromium();
  if (manageDocker) {
    await dockerCompose(['down'], { allowFailure: true });
    await rm(overrideFile, { force: true });
  }
}

async function startDocker() {
  await writeFile(overrideFile, [
    'services:',
    '  anycode:',
    '    volumes:',
    `      - ${codexHome}:/root/.codex`,
    '',
  ].join('\n'));
  await dockerCompose(['up', '-d', '--build']);
}

async function prepareContainerWorkspaces() {
  const script = `
set -eu
mkdir -p ${shellQuote(plainPath)}
printf 'AnyCode plain E2E workspace\\n' > ${shellQuote(`${plainPath}/README.md`)}
rm -rf ${shellQuote(gitPath)}
mkdir -p ${shellQuote(gitPath)}
cd ${shellQuote(gitPath)}
git init -b main >/dev/null
git config user.email anycode-e2e@example.local
git config user.name 'AnyCode E2E'
printf 'AnyCode git E2E workspace\\n' > README.md
git add README.md
git commit -m 'init e2e repo' >/dev/null
chown -R anycode:anycode ${shellQuote(plainPath)} ${shellQuote(gitPath)}
`;
  await dockerCompose(['exec', '-T', 'anycode', 'sh', '-lc', script]);
}

async function dockerCompose(composeArgs, options = {}) {
  const env = { ...process.env, ANYCODE_ACCESS_KEY: accessKey };
  const allArgs = ['compose', '-f', 'compose.yml', '-f', overrideFile, ...composeArgs];
  const result = await run('docker', allArgs, { env, allowFailure: options.allowFailure });
  return result.stdout;
}

async function waitForHTTPHealth() {
  const started = Date.now();
  while (Date.now() - started < 60_000) {
    try {
      const response = await fetch(`${baseURL}/healthz`);
      if (response.status === 204) return;
    } catch {
      // keep polling
    }
    await sleep(500);
  }
  throw new Error(`Timed out waiting for ${baseURL}/healthz`);
}

function launchChromium() {
  const proc = spawn('/bin/chromium', [
    '--headless=new',
    '--disable-gpu',
    '--no-sandbox',
    '--disable-dev-shm-usage',
    `--remote-debugging-port=${debugPort}`,
    `--user-data-dir=${userDataDir}`,
    'about:blank',
  ], { stdio: ['ignore', 'pipe', 'pipe'] });
  let output = '';
  proc.stdout.on('data', (chunk) => { output += chunk.toString(); });
  proc.stderr.on('data', (chunk) => { output += chunk.toString(); });
  proc.output = () => output;
  return proc;
}

async function stopChromium() {
  if (!chrome || chrome.exitCode !== null) return;
  chrome.kill('SIGTERM');
  await Promise.race([
    new Promise((resolve) => chrome.once('exit', resolve)),
    sleep(1000).then(() => {
      if (chrome.exitCode === null) chrome.kill('SIGKILL');
    }),
  ]);
  await rm(userDataDir, { recursive: true, force: true }).catch(() => {});
}

async function waitForChrome() {
  const started = Date.now();
  while (Date.now() - started < 20_000) {
    try {
      const response = await fetch(`${debugURL}/json/list`);
      if (response.ok) return;
    } catch {
      // keep polling
    }
    if (chrome.exitCode !== null) {
      throw new Error(`Chromium exited early: ${chrome.exitCode}\n${chrome.output?.() || ''}`);
    }
    await sleep(250);
  }
  throw new Error('Timed out waiting for Chromium CDP');
}

async function connectPage() {
  const pages = await fetch(`${debugURL}/json/list`).then((response) => response.json());
  const target = pages.find((item) => item.type === 'page') || pages[0];
  assert(target?.webSocketDebuggerUrl, 'No Chromium page target found');
  const ws = new WebSocket(target.webSocketDebuggerUrl);
  const pending = new Map();
  const listeners = new Map();
  let nextId = 1;
  await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('Timed out opening CDP websocket')), 10_000);
    ws.addEventListener('open', () => {
      clearTimeout(timer);
      resolve();
    }, { once: true });
    ws.addEventListener('error', reject, { once: true });
  });
  ws.addEventListener('message', (message) => {
    const payload = JSON.parse(message.data);
    if (payload.id && pending.has(payload.id)) {
      const { resolve, reject } = pending.get(payload.id);
      pending.delete(payload.id);
      if (payload.error) reject(new Error(JSON.stringify(payload.error)));
      else resolve(payload.result || {});
      return;
    }
    if (payload.method && listeners.has(payload.method)) {
      for (const listener of listeners.get(payload.method)) listener(payload.params || {});
    }
  });
  return {
    send(method, params = {}) {
      const id = nextId++;
      ws.send(JSON.stringify({ id, method, params }));
      return new Promise((resolve, reject) => {
        const timer = setTimeout(() => {
          pending.delete(id);
          reject(new Error(`CDP command timed out: ${method}`));
        }, 30_000);
        pending.set(id, {
          resolve: (value) => {
            clearTimeout(timer);
            resolve(value);
          },
          reject: (error) => {
            clearTimeout(timer);
            reject(error);
          },
        });
      });
    },
    once(method, timeoutMs = 30_000) {
      return new Promise((resolve, reject) => {
        const timer = setTimeout(() => {
          remove();
          reject(new Error(`Timed out waiting for event: ${method}`));
        }, timeoutMs);
        const listener = (params) => {
          clearTimeout(timer);
          remove();
          resolve(params);
        };
        const remove = () => {
          const current = listeners.get(method) || [];
          listeners.set(method, current.filter((item) => item !== listener));
        };
        listeners.set(method, [...(listeners.get(method) || []), listener]);
      });
    },
    on(method, listener) {
      listeners.set(method, [...(listeners.get(method) || []), listener]);
    },
    close() {
      ws.close();
    },
  };
}

function trackBrowserFailures(browserPage) {
  const state = {
    consoleErrors: [],
    pageErrors: [],
    failedRequests: [],
  };
  browserPage.on('Runtime.exceptionThrown', (event) => {
    state.pageErrors.push(event.exceptionDetails?.text || 'runtime exception');
  });
  browserPage.on('Log.entryAdded', (event) => {
    if (event.entry?.level === 'error') state.consoleErrors.push(event.entry.text);
  });
  browserPage.on('Network.loadingFailed', (event) => {
    const type = event.type || '';
    const reason = event.errorText || '';
    if (type !== 'WebSocket' && reason !== 'net::ERR_ABORTED') {
      state.failedRequests.push(`${type}: ${reason}`);
    }
  });
  return {
    clear() {
      state.consoleErrors = [];
      state.pageErrors = [];
      state.failedRequests = [];
    },
    assertClean() {
      if (state.pageErrors.length || state.consoleErrors.length || state.failedRequests.length) {
        throw new Error(JSON.stringify(state, null, 2));
      }
    },
    snapshot() {
      return {
        consoleErrors: [...state.consoleErrors],
        pageErrors: [...state.pageErrors],
        failedRequests: [...state.failedRequests],
      };
    },
  };
}

async function setViewport(width, height) {
  await page.send('Emulation.setDeviceMetricsOverride', {
    width,
    height,
    deviceScaleFactor: 1,
    mobile: width < 600,
  });
}

async function navigate(path) {
  const url = path.startsWith('http') ? path : `${baseURL}${path}`;
  const loaded = page.once('Page.loadEventFired', 30_000).catch(() => null);
  const result = await page.send('Page.navigate', { url });
  if (result.loaderId) await loaded;
  await waitForReadyState();
  await sleep(500);
}

async function waitForReadyState(timeoutMs = 20_000) {
  const started = Date.now();
  while (Date.now() - started < timeoutMs) {
    const ready = await evaluate('document.readyState');
    if (ready === 'interactive' || ready === 'complete') return;
    await sleep(200);
  }
  throw new Error('Timed out waiting for document readiness');
}

async function evaluate(expression) {
  const result = await page.send('Runtime.evaluate', {
    expression,
    awaitPromise: true,
    returnByValue: true,
  });
  if (result.exceptionDetails) {
    throw new Error(result.exceptionDetails.text || 'Runtime.evaluate failed');
  }
  return result.result?.value;
}

async function waitForText(text, timeoutMs = 25_000) {
  const escaped = JSON.stringify(text);
  const started = Date.now();
  while (Date.now() - started < timeoutMs) {
    const found = await evaluate(`document.body && document.body.innerText.includes(${escaped})`);
    if (found) return;
    await sleep(250);
  }
  const body = await evaluate('document.body ? document.body.innerText.slice(0, 1600) : ""');
  throw new Error(`Timed out waiting for text ${text}. Body: ${body}\nBrowser failures: ${JSON.stringify(browserFailures?.snapshot?.() || {}, null, 2)}`);
}

async function waitForVisibleSelector(selector, timeoutMs = 10_000) {
  const started = Date.now();
  while (Date.now() - started < timeoutMs) {
    const visible = await evaluate(`(() => {
      const element = document.querySelector(${JSON.stringify(selector)});
      if (!element) return false;
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0;
    })()`);
    if (visible) return;
    await sleep(250);
  }
  throw new Error(`Timed out waiting for visible selector ${selector}`);
}

async function waitForCondition(expression, label, timeoutMs = 10_000) {
  const started = Date.now();
  while (Date.now() - started < timeoutMs) {
    if (await evaluate(expression)) return;
    await sleep(250);
  }
  throw new Error(`Timed out waiting for ${label}`);
}

async function clickText(text) {
  const clicked = await evaluate(`(() => {
    const needle = ${JSON.stringify(text)}.toLocaleLowerCase();
    const target = Array.from(document.querySelectorAll('button, [role="button"], [role="tab"], .q-tab, .q-item'))
      .find((element) => {
        const rect = element.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 &&
          rect.left < innerWidth && rect.top < innerHeight &&
          element.innerText && element.innerText.toLocaleLowerCase().includes(needle);
      });
    if (!target) return false;
    target.click();
    return true;
  })()`);
  assert(clicked, `click target not found: ${text}`);
  await sleep(300);
}

async function clickTab(label) {
  const clicked = await evaluate(`(() => {
    const needle = ${JSON.stringify(label)}.toLocaleLowerCase();
    const target = Array.from(document.querySelectorAll('[role="tab"]')).find((element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && element.innerText.toLocaleLowerCase().includes(needle);
    });
    if (!target) return false;
    target.click();
    return true;
  })()`);
  assert(clicked, `tab not found: ${label}`);
  await sleep(300);
}

async function clickAttachment(filename) {
  const clicked = await evaluate(`(() => {
    const target = Array.from(document.querySelectorAll('.attachment-chip'))
      .find((element) => element.textContent?.includes(${JSON.stringify(filename)}));
    if (!target) return false;
    target.click();
    return true;
  })()`);
  assert(clicked, `attachment chip not found: ${filename}`);
  await sleep(300);
}

async function clickAria(label) {
  const clicked = await evaluate(`(() => {
    const target = Array.from(document.querySelectorAll('[aria-label=${JSON.stringify(label)}]'))
      .find((element) => {
        const rect = element.getBoundingClientRect();
        return !element.disabled && element.getAttribute('aria-disabled') !== 'true' &&
          rect.width > 0 && rect.height > 0;
      });
    if (!target) return false;
    target.scrollIntoView({ block: 'center', inline: 'nearest' });
    target.click();
    return true;
  })()`);
  assert(clicked, `aria target not found: ${label}`);
  await sleep(300);
}

async function closeVisibleDialog() {
  const result = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 &&
        rect.left < innerWidth && rect.top < innerHeight;
    };
    const dialogs = Array.from(document.querySelectorAll('.q-dialog')).filter(visible).reverse();
    const target = dialogs
      .flatMap((dialog) => Array.from(dialog.querySelectorAll('button')))
      .find((button) => visible(button) && (
        button.getAttribute('aria-label')?.startsWith('关闭') ||
        button.getAttribute('aria-label') === '取消' ||
        button.innerText.trim() === '取消' ||
        button.innerText.trim().toLocaleLowerCase() === 'cancel'
      ));
    if (!target) return { closed: false, count: dialogs.length };
    target.click();
    return { closed: true, count: dialogs.length };
  })()`);
  if (!result.closed) {
    assert(result.count > 0, 'visible dialog not found');
    await page.send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Escape', code: 'Escape' });
    await page.send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Escape', code: 'Escape' });
    await waitForCondition(`Array.from(document.querySelectorAll('.q-dialog')).filter((element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 &&
        rect.left < innerWidth && rect.top < innerHeight;
    }).length < ${result.count}`, 'dialog closed with Escape');
  }
  await sleep(300);
}

async function assertSessionCardShowsProject(marker, projectName) {
  const result = await evaluate(`(() => {
    const card = Array.from(document.querySelectorAll('.overview-session-card'))
      .find((element) => element.innerText.includes(${JSON.stringify(marker)}));
    return {
      found: Boolean(card),
      cardText: card?.innerText || '',
    };
  })()`);
  assert(result.found, `overview card not found for ${marker}`);
  assert(result.cardText.includes(projectName), `card is missing project name ${projectName}: ${result.cardText}`);
}

async function assertProjectChipPersistence(projectId, projectName, marker) {
  await clickAria(`隐藏 ${projectName} 项目卡片`);
  const hidden = await evaluate(`(() => ({
    chipVisible: Boolean(document.querySelector('[aria-label=${JSON.stringify(`显示 ${projectName} 项目卡片`)}]')),
    cardVisible: Array.from(document.querySelectorAll('.overview-session-card'))
      .some((card) => card.innerText && card.innerText.includes(${JSON.stringify(marker)})),
    stored: JSON.parse(localStorage.getItem('anycode.overview.hidden-projects.v1') || '[]'),
  }))()`);
  assert(hidden.chipVisible, `${projectName} hidden-state chip is missing`);
  assert(!hidden.cardVisible, `${projectName} card remained visible after hiding`);
  assert(hidden.stored.includes(projectId), `${projectName} hidden state was not persisted`);

  await navigate('/');
  await clickAria(`显示 ${projectName} 项目卡片`);
  await waitForText(marker);
}

async function assertSessionsTableProjectName(marker, projectName, projectId) {
  const result = await evaluate(`(() => {
    const row = Array.from(document.querySelectorAll('tbody tr, .q-table__grid-content .q-card, .q-item'))
      .find((element) => element.innerText && element.innerText.includes(${JSON.stringify(marker)}));
    return {
      found: Boolean(row),
      rowText: row?.innerText || '',
    };
  })()`);
  assert(result.found, `sessions row not found for ${marker}`);
  assert(result.rowText.includes(projectName), `sessions row missing project name ${projectName}: ${result.rowText}`);
  assert(!result.rowText.includes(projectId), `sessions row still shows project id ${projectId}: ${result.rowText}`);
}

async function assertNewSessionDefaultProject({ route, expectedProjectName, label, screenshotName, withAttachments = false }) {
  await navigate(route);
  await waitForVisibleSelector('.overview-filter-toolbar');
  const composerVisible = await evaluate(`Boolean(document.querySelector('.new-session-dialog'))`);
  const modalOpened = !composerVisible;
  if (modalOpened) await clickAria('新建卡片');
  await waitForVisibleSelector('.new-session-dialog');
  if (withAttachments) {
    await attachNewSessionFixtureFiles();
    await waitForText('notes.md');
    await waitForText('screenshot.png');
    await waitForText('demo.mp4');
    await waitForText('schema.sql');
  }
  const dialogText = await evaluate(`(() => {
    const dialog = Array.from(document.querySelectorAll('.q-dialog')).find((element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0;
    });
    return dialog?.innerText || '';
  })()`);
  assert(dialogText.includes(expectedProjectName), `${label} did not select ${expectedProjectName}: ${dialogText}`);
  if (screenshotName) await screenshot(screenshotName);
  if (modalOpened) await closeVisibleDialog();
}

async function attachNewSessionFixtureFiles() {
  const fixtureDir = `/tmp/anycode-e2e-attachments-${stamp}`;
  await mkdir(fixtureDir, { recursive: true });
  const files = [
    `${fixtureDir}/notes.md`,
    `${fixtureDir}/screenshot.png`,
    `${fixtureDir}/demo.mp4`,
    `${fixtureDir}/schema.sql`,
  ];
  await writeFile(files[0], '# E2E notes\n\nCheck attachment chip layout.\n');
  await writeFile(files[1], 'AnyCode screenshot attachment placeholder\n');
  await writeFile(files[2], 'AnyCode video attachment placeholder\n');
  await writeFile(files[3], 'create table anycode_e2e_attachment(id text primary key);\n');

  const documentNode = await page.send('DOM.getDocument', { depth: -1, pierce: true });
  const inputNode = await page.send('DOM.querySelector', {
    nodeId: documentNode.root.nodeId,
    selector: '.new-session-dialog input[type="file"]',
  });
  assert(inputNode.nodeId, 'new session file input not found');
  await page.send('DOM.setFileInputFiles', {
    nodeId: inputNode.nodeId,
    files,
  });
  await sleep(500);
}

async function screenshotDirectoryDialog() {
  await clickAria('更多操作');
  await auditDarkThemeSurface('menu-header-more');
  await clickText('全局设置');
  await waitForText('全局设置');
  await auditDarkThemeSurface('dialog-global-settings', 'projects');
  await clickAria('新增项目');
  await waitForText('选择项目目录');
  await waitForText('当前路径');
  await screenshot('10-directory-dialog.png');
  await auditDarkThemeSurface('dialog-project-directory');
  await closeVisibleDialog();
  await closeVisibleDialog();
}

async function screenshotSessionDiffWorkspace(filePath) {
  await waitForVisibleSelector('.detail-diff-panel .diff-workspace');
  await waitForCondition(
    `Array.from(document.querySelectorAll('.detail-diff-panel .diff-file-card'))
      .some((element) => element.innerText.includes(${JSON.stringify(filePath)}))`,
    `session detail diff contains ${filePath}`,
  );
  await screenshot('13-session-diff-workspace.png');
}

async function assertSessionDetailReadableLayout(label) {
  const result = await evaluate(`(() => {
    const doc = document.documentElement;
    const page = document.querySelector('.detail-page');
    const pageContainer = document.querySelector('.q-page-container');
    const eventTexts = Array.from(document.querySelectorAll(
      '.text-message__main, .status-event__content, .tool-event__header span, .command-event__title, .reasoning-event__header span',
    ))
      .map((element) => element.innerText.trim())
      .filter(Boolean);
    const jsonLike = eventTexts.filter((text) => text.startsWith('{') || text.includes('"processRunId"') || text.includes('"raw"'));
    const stream = document.querySelector('.stream-card__body') || document.querySelector('.stream-card');
    const streamStyle = stream ? getComputedStyle(stream) : null;
    const visibleText = document.body.innerText;
    return {
      eventCount: eventTexts.length,
      jsonLike,
      hasEventStreamHeader: visibleText.includes('会话事件流'),
      hasNormalExitText: visibleText.includes('正常退出') || visibleText.includes('退出码 0'),
      expandedToolBodies: document.querySelectorAll('.tool-event__content, .command-event__body').length,
      pageScrollHeight: doc.scrollHeight,
      innerHeight: window.innerHeight,
      bodyScrolls: doc.scrollHeight > window.innerHeight + 1,
      pageScrolls: page ? page.scrollHeight > page.clientHeight + 1 : false,
      pageContainerScrolls: pageContainer ? pageContainer.scrollHeight > pageContainer.clientHeight + 1 : false,
      streamOverflowY: streamStyle?.overflowY || '',
      streamCanScroll: stream ? stream.scrollHeight >= stream.clientHeight : false,
    };
  })()`);
  assert(result.eventCount > 0, `${label} has no event text`);
  assert(result.jsonLike.length === 0, `${label} still shows JSON event payload: ${result.jsonLike.join('\\n')}`);
  assert(!result.hasEventStreamHeader, `${label} still shows the event stream header`);
  assert(!result.hasNormalExitText, `${label} still shows normal exit text`);
  assert(result.expandedToolBodies === 0, `${label} tool messages are not collapsed by default`);
  assert(!result.bodyScrolls, `${label} scrolls the whole page: ${result.pageScrollHeight} > ${result.innerHeight}`);
  assert(!result.pageScrolls, `${label} scrolls the detail page`);
  assert(!result.pageContainerScrolls, `${label} scrolls the page container`);
  assert(['auto', 'scroll'].includes(result.streamOverflowY), `${label} event stream is not scrollable`);
}

async function assertSessionDetailResizableLayout(firstSessionId, secondSessionId) {
  const storageKey = 'anycode:session-detail:right-panel-width';
  await setViewport(1440, 900);
  await evaluate(`localStorage.removeItem(${JSON.stringify(storageKey)})`);
  await navigate(`/#/sessions/${firstSessionId}`);
  await waitForVisibleSelector('.detail-splitter__handle');

  const initial = await readSessionSplitterMetrics(storageKey);
  assert(Math.abs(initial.rightWidth - 360) <= 1, `splitter default width = ${initial.rightWidth}`);
  assert(initial.leftWidth >= 480, `splitter default left width = ${initial.leftWidth}`);
  assert(initial.separatorWidth === 16, `splitter separator width = ${initial.separatorWidth}`);
  await screenshot('14-session-splitter-default.png');

  await evaluate(`localStorage.setItem(${JSON.stringify(storageKey)}, ' ')`);
  await navigate(`/#/sessions/${firstSessionId}`);
  await waitForVisibleSelector('.detail-splitter__handle');
  const invalidPreference = await readSessionSplitterMetrics(storageKey);
  assert(
    Math.abs(invalidPreference.rightWidth - 360) <= 1,
    `splitter invalid preference width = ${invalidPreference.rightWidth}`,
  );

  const dragStart = await readSessionSplitterMetrics(storageKey);
  const startX = dragStart.handleLeft + dragStart.handleWidth / 2;
  const startY = dragStart.handleTop + dragStart.handleHeight / 2;
  await page.send('Input.dispatchMouseEvent', { type: 'mouseMoved', x: startX, y: startY });
  await page.send('Input.dispatchMouseEvent', {
    type: 'mousePressed',
    x: startX,
    y: startY,
    button: 'left',
    buttons: 1,
    clickCount: 1,
  });
  await page.send('Input.dispatchMouseEvent', {
    type: 'mouseMoved',
    x: startX - 48,
    y: startY,
    button: 'left',
    buttons: 1,
  });
  await page.send('Input.dispatchMouseEvent', {
    type: 'mouseMoved',
    x: startX - 96,
    y: startY,
    button: 'left',
    buttons: 1,
  });
  await page.send('Input.dispatchMouseEvent', {
    type: 'mouseReleased',
    x: startX - 96,
    y: startY,
    button: 'left',
    buttons: 0,
    clickCount: 1,
  });
  await waitForCondition(
    `Number(localStorage.getItem(${JSON.stringify(storageKey)})) > 360`,
    'session splitter drag persistence',
  );

  const dragged = await readSessionSplitterMetrics(storageKey);
  assert(dragged.rightWidth > 360, `splitter drag width = ${dragged.rightWidth}`);
  assert(dragged.leftWidth >= 480, `splitter drag left width = ${dragged.leftWidth}`);
  await evaluate(`document.querySelector('.detail-splitter__handle').focus()`);
  await page.send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'ArrowRight', code: 'ArrowRight' });
  await page.send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'ArrowRight', code: 'ArrowRight' });
  await sleep(200);
  const keyboard = await readSessionSplitterMetrics(storageKey);
  assert(
    Math.abs(keyboard.rightWidth - (dragged.rightWidth - 16)) <= 1,
    `splitter keyboard width ${keyboard.rightWidth} did not follow ${dragged.rightWidth}`,
  );
  assert(keyboard.focused, 'splitter keyboard handle lost focus');
  assert(keyboard.focusRing !== 'none', 'splitter focus ring is not visible');

  await navigate(`/#/sessions/${secondSessionId}`);
  await waitForVisibleSelector('.detail-splitter__handle');
  const persisted = await readSessionSplitterMetrics(storageKey);
  assert(
    Math.abs(persisted.rightWidth - keyboard.rightWidth) <= 1,
    `splitter persisted width ${persisted.rightWidth} != ${keyboard.rightWidth}`,
  );
  await screenshot('15-session-splitter-persisted.png');

  await evaluate(`localStorage.setItem(${JSON.stringify(storageKey)}, '700')`);
  const reloaded = page.once('Page.loadEventFired', 30_000);
  await page.send('Page.reload', { ignoreCache: true });
  await reloaded;
  await waitForReadyState();
  await waitForVisibleSelector('.detail-splitter__handle');
  const widePreference = await readSessionSplitterMetrics(storageKey);
  assert(Math.abs(widePreference.rightWidth - 700) <= 1, `splitter wide width = ${widePreference.rightWidth}`);

  await setViewport(1024, 768);
  await sleep(400);
  const clamped = await readSessionSplitterMetrics(storageKey);
  assert(Math.abs(clamped.leftWidth - 480) <= 1, `splitter clamped left width = ${clamped.leftWidth}`);
  assert(Math.abs(clamped.rightWidth - 480) <= 1, `splitter clamped right width = ${clamped.rightWidth}`);
  assert(clamped.storedWidth === 700, `splitter clamp overwrote preference: ${clamped.storedWidth}`);
  assert(!clamped.hasHorizontalOverflow, 'splitter clamped viewport has horizontal overflow');
  await screenshot('16-session-splitter-clamped.png');

  await setViewport(2048, 1152);
  await sleep(400);
  const restored = await readSessionSplitterMetrics(storageKey);
  assert(Math.abs(restored.rightWidth - 700) <= 1, `splitter restored width = ${restored.rightWidth}`);
  assert(restored.leftWidth >= 480, `splitter restored left width = ${restored.leftWidth}`);
  await screenshot('17-session-splitter-restored.png');

  await setViewport(1023, 900);
  await sleep(400);
  let mobile = await readSessionSplitterMetrics(storageKey);
  assert(mobile.separatorDisplay === 'none', `mobile splitter separator = ${mobile.separatorDisplay}`);
  assert(mobile.visiblePanelCount === 1, `mobile splitter visible panels = ${mobile.visiblePanelCount}`);
  assert(!mobile.hasHorizontalOverflow, 'mobile splitter viewport has horizontal overflow');
  for (const label of ['信息', '变更', '产物', '会话']) {
    await clickTab(label);
    mobile = await readSessionSplitterMetrics(storageKey);
    assert(mobile.visiblePanelCount === 1, `mobile ${label} visible panels = ${mobile.visiblePanelCount}`);
    assert(!mobile.hasHorizontalOverflow, `mobile ${label} viewport has horizontal overflow`);
  }
  await screenshot('18-session-splitter-mobile.png');

  await evaluate(`localStorage.setItem(${JSON.stringify(storageKey)}, '360')`);
  await setViewport(1440, 900);
}

async function readSessionSplitterMetrics(storageKey) {
  return evaluate(`(() => {
    const root = document.querySelector('.detail-splitter');
    const left = root?.querySelector('.q-splitter__before');
    const right = root?.querySelector('.q-splitter__after');
    const separator = root?.querySelector('.q-splitter__separator');
    const handle = root?.querySelector('.detail-splitter__handle');
    if (!root || !left || !right || !separator || !handle) throw new Error('session splitter missing');
    const leftRect = left.getBoundingClientRect();
    const rightRect = right.getBoundingClientRect();
    const separatorRect = separator.getBoundingClientRect();
    const handleRect = handle.getBoundingClientRect();
    const panels = [left, right];
    return {
      leftWidth: leftRect.width,
      rightWidth: rightRect.width,
      separatorWidth: separatorRect.width,
      separatorDisplay: getComputedStyle(separator).display,
      handleLeft: handleRect.left,
      handleTop: handleRect.top,
      handleWidth: handleRect.width,
      handleHeight: handleRect.height,
      focused: document.activeElement === handle,
      focusRing: getComputedStyle(handle).boxShadow,
      visiblePanelCount: panels.filter((panel) => {
        const rect = panel.getBoundingClientRect();
        return getComputedStyle(panel).display !== 'none' && rect.width > 0 && rect.height > 0;
      }).length,
      storedWidth: Number(localStorage.getItem(${JSON.stringify(storageKey)})),
      hasHorizontalOverflow: document.documentElement.scrollWidth > document.documentElement.clientWidth,
    };
  })()`);
}

async function clickQuestionButtonForCard(marker) {
  const clicked = await evaluate(`(() => {
    const card = Array.from(document.querySelectorAll('.overview-session-card, .lane-session-card'))
      .find((element) => element.innerText.includes(${JSON.stringify(marker)}));
    const button = card?.querySelector('[aria-label="回答问题"]');
    if (!button) return false;
    button.click();
    return true;
  })()`);
  assert(clicked, `question button not found for card ${marker}`);
  await sleep(500);
}

async function assertNoHorizontalOverflow(label) {
  const overflow = await evaluate(`(() => {
    const doc = document.documentElement;
    return {
      scrollWidth: doc.scrollWidth,
      innerWidth: window.innerWidth,
      overflow: doc.scrollWidth > window.innerWidth + 1,
    };
  })()`);
  assert(!overflow.overflow, `${label} has horizontal overflow: ${overflow.scrollWidth} > ${overflow.innerWidth}`);
}

async function auditDarkThemeSurface(surfaceId, stateId = 'default') {
  if (!darkThemeAudit) return;
  await darkThemeAudit.captureAllViewports(surfaceId, stateId);
  await setViewport(1440, 900);
  await sleep(300);
}

async function auditDarkThemeRoutes({ gitProject, gitSession }) {
  const replacements = {
    ':projectId': gitProject.id,
    ':sessionId': gitSession.id,
  };
  for (const route of darkThemeRoutes) {
    let path = route.path;
    for (const [placeholder, value] of Object.entries(replacements)) {
      path = path.replaceAll(placeholder, value);
    }
    if (route.id === 'login') {
      await evaluate(`localStorage.removeItem('anycode.accessKey')`);
      await navigate(path);
    } else {
      await evaluate(`localStorage.setItem('anycode.accessKey', ${JSON.stringify(accessKey)})`);
      await navigate(path);
    }
    await waitForCondition('document.body && document.body.innerText.trim().length > 0', `${route.id} rendered`);
    if (route.id === 'diff') {
      await darkThemeAudit.captureAllViewports('route-diff', 'all');
      await navigate(`${path}&mode=single&filePath=e2e-codex-output.txt`);
      await waitForText('e2e-codex-output.txt');
      await darkThemeAudit.captureAllViewports('route-diff', 'single');
    } else {
      await darkThemeAudit.captureAllViewports(`route-${route.id}`);
    }
  }
  await evaluate(`localStorage.setItem('anycode.accessKey', ${JSON.stringify(accessKey)})`);
  await setViewport(1440, 900);
}

async function auditDarkThemeDialogsAndMenus({ gitProject, gitSession, approvalSession }) {
  await setViewport(390, 844);
  await navigate('/');
  await clickAria('新建卡片');
  await waitForVisibleSelector('.new-session-dialog');
  await attachNewSessionFixtureFiles();
  await auditDarkThemeSurface('dialog-new-session');
  await setViewport(390, 844);
  await sleep(300);
  await auditDarkThemeSelectPopup();
  await clickAttachment('screenshot.png');
  await waitForVisibleSelector('.attachment-preview-card');
  await auditDarkThemeSurface('dialog-prompt-attachment-preview');
  await clickAria('关闭预览');
  await setViewport(390, 844);
  await sleep(300);
  await closeVisibleDialog();

  await setViewport(1440, 900);
  await navigate('/');
  await clickAria('更多操作');
  await clickText('全局设置');
  await waitForVisibleSelector('.global-settings-dialog');
  await clickText('快捷指令');
  await waitForText('暂无快捷指令');
  await auditDarkThemeSurface('dialog-global-settings', 'quick-commands');
  await clickText('项目');
  await waitForCondition(
    `Boolean(document.querySelector('[aria-label=${JSON.stringify(`${gitProject.name} 项目操作`)}]'))`,
    'global settings project actions',
  );

  await clickAria(`${gitProject.name} 项目操作`);
  await auditDarkThemeSurface('menu-global-project-actions');
  await clickText('设置');
  await waitForVisibleSelector('.project-settings-dialog');
  await auditDarkThemeSurface('dialog-project-settings');
  await closeVisibleDialog();

  await clickAria(`${gitProject.name} 项目操作`);
  await clickText('移除项目');
  await waitForText('确认移除项目');
  await auditDarkThemeSurface('dialog-remove-project-confirmation');
  await closeVisibleDialog();
  await closeVisibleDialog();

  await clickAria('更多操作');
  await clickText('退出');
  await waitForText('退出登录');
  await auditDarkThemeSurface('dialog-logout-confirmation');
  await closeVisibleDialog();

  await navigate('/');
  await clickAria('人工审核');
  await waitForVisibleSelector('.forward-approval-dialog');
  await auditDarkThemeSurface('dialog-forward-approval', 'result');
  await clickTab('Diff');
  await waitForVisibleSelector('.forward-approval-dialog .diff-workspace');
  await auditDarkThemeSurface('dialog-forward-approval', 'diff');
  await closeVisibleDialog();

  await openOverviewDiffForCard('ANYCODE_GIT_E2E_OK');
  await waitForVisibleSelector('.overview-diff-dialog');
  await auditDarkThemeSurface('dialog-overview-diff');
  await closeVisibleDialog();

  await navigate(`/#/sessions/${gitSession.id}`);
  await waitForText('E2E 追加描述验证');
  await clickAria('编辑追加提示');
  await waitForVisibleSelector('.prompt-edit-dialog');
  await auditDarkThemeSurface('dialog-edit-prompt-append');
  await closeVisibleDialog();

  await publishThemeAuditArtifact(gitSession.id);
  await navigate(`/#/sessions/${gitSession.id}`);
  await clickText('产物');
  await waitForText('theme-audit.txt');
  await clickArtifactListItem('theme-audit.txt');
  await waitForVisibleSelector('.artifact-preview-dialog');
  await auditDarkThemeSurface('dialog-session-artifact-preview');
  await closeVisibleDialog();

  await clickAria('删除文件');
  await waitForText('删除产物');
  await auditDarkThemeSurface('dialog-delete-artifact-confirmation');
  await closeVisibleDialog();

  await clickAria('预览产物');
  await waitForVisibleSelector('.artifact-event-preview');
  await auditDarkThemeSurface('dialog-timeline-artifact-preview');
  await closeVisibleDialog();

  await auditDarkThemeMenus(gitSession.id);
  await auditDarkThemeTooltipAndNotification();
  assert(approvalSession.id, 'approval session fixture is required for dark theme audit');
}

async function openFirstVisibleSelect() {
  const opened = await evaluate(`(() => {
    const select = Array.from(document.querySelectorAll('.q-select')).find((element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && !element.classList.contains('disabled');
    });
    if (!select) return false;
    select.click();
    return true;
  })()`);
  assert(opened, 'visible select popup trigger is missing');
  await waitForCondition(`Array.from(document.querySelectorAll('.q-menu')).some((element) => {
    const rect = element.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0;
  })`, 'select popup');
}

async function auditDarkThemeSelectPopup() {
  for (const viewport of darkThemeViewports) {
    await setViewport(viewport.width, viewport.height);
    await openFirstVisibleSelect();
    await darkThemeAudit.capture('overlay-select-popup', viewport);
    await page.send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Escape', code: 'Escape' });
    await page.send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Escape', code: 'Escape' });
  }
}

async function auditDarkThemeTooltipAndNotification() {
  for (const viewport of darkThemeViewports) {
    await setViewport(viewport.width, viewport.height);
    await navigate('/');
    const rect = await evaluate(`(() => {
      const target = document.querySelector('[aria-label="历史卡片"]');
      if (!target) return null;
      const box = target.getBoundingClientRect();
      return { x: box.left + box.width / 2, y: box.top + box.height / 2 };
    })()`);
    assert(rect, 'tooltip trigger is missing');
    await page.send('Input.dispatchMouseEvent', { type: 'mouseMoved', x: rect.x, y: rect.y });
    await waitForCondition(`Array.from(document.querySelectorAll('.q-tooltip')).some((element) => {
      const box = element.getBoundingClientRect();
      return box.width > 0 && box.height > 0;
    })`, 'tooltip visible');
    await darkThemeAudit.capture('overlay-tooltip', viewport);
    await page.send('Input.dispatchMouseEvent', { type: 'mouseMoved', x: 0, y: 0 });

    await evaluate(`localStorage.removeItem('anycode.accessKey')`);
    await navigate('/#/login');
    await clickText('进入');
    await waitForCondition(`Array.from(document.querySelectorAll('.q-notification')).some((element) => {
      const box = element.getBoundingClientRect();
      return box.width > 0 && box.height > 0;
    })`, 'notification visible');
    await darkThemeAudit.capture('overlay-notification', viewport);
    await evaluate(`localStorage.setItem('anycode.accessKey', ${JSON.stringify(accessKey)})`);
  }
}

async function openOverviewDiffForCard(marker) {
  const clicked = await evaluate(`(() => {
    const card = Array.from(document.querySelectorAll('.overview-session-card'))
      .find((element) => element.innerText.includes(${JSON.stringify(marker)}));
    const button = card?.querySelector('.overview-diff-btn');
    if (!button) return false;
    button.click();
    return true;
  })()`);
  assert(clicked, `Diff button not found for ${marker}`);
  await sleep(500);
}

async function clickArtifactListItem(filename) {
  const clicked = await evaluate(`(() => {
    const item = Array.from(document.querySelectorAll('.artifact-list .q-item'))
      .find((element) => element.innerText.includes(${JSON.stringify(filename)}));
    if (!item) return false;
    item.click();
    return true;
  })()`);
  assert(clicked, `artifact list item not found: ${filename}`);
  await sleep(500);
}

async function publishThemeAuditArtifact(sessionId) {
  const relativePath = `attachments/outputs/${sessionId}/theme-audit.txt`;
  if (manageDocker) {
    const artifactPath = `/home/anycode/.anycode/${relativePath}`;
    await dockerCompose([
      'exec',
      '-T',
      'anycode',
      'sh',
      '-lc',
      `mkdir -p ${shellQuote(artifactPath.slice(0, artifactPath.lastIndexOf('/')))} && printf 'dark theme audit\\n' > ${shellQuote(artifactPath)}`,
    ]);
  } else {
    if (!e2eDataDir) throw new Error('ANYCODE_E2E_DATA_DIR is required without --manage-docker');
    const artifactPath = `${e2eDataDir}/${relativePath}`;
    await mkdir(artifactPath.slice(0, artifactPath.lastIndexOf('/')), { recursive: true });
    await writeFile(artifactPath, 'dark theme audit\n');
  }
  const response = await callSessionMCP(sessionId, 'publish_artifact', {
    path: 'theme-audit.txt',
    logicalPath: 'theme-audit.txt',
    correlationId: `theme-audit-${stamp}`,
  });
  assert(response.status === 200, `publish_artifact status = ${response.status}: ${JSON.stringify(response.body)}`);
}

async function auditDarkThemeMenus(sessionId) {
  await navigate('/');
  const todoOpened = await evaluate(`(() => {
    const button = document.querySelector('[aria-label="查看 TODO List"]');
    if (!button) return false;
    button.click();
    return true;
  })()`);
  assert(todoOpened, 'overview TODO menu fixture is missing');
  await auditDarkThemeSurface('menu-overview-todo');
  await page.send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Escape', code: 'Escape' });
  await page.send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Escape', code: 'Escape' });

  const contextOpened = await evaluate(`(() => {
    const card = document.querySelector('.overview-session-card');
    if (!card) return false;
    card.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, button: 2 }));
    return true;
  })()`);
  assert(contextOpened, 'overview context menu fixture is missing');
  await waitForCondition(`Array.from(document.querySelectorAll('.q-menu')).some((element) => element.innerText.includes('在新标签页中打开'))`, 'overview context menu');
  await auditDarkThemeSurface('menu-overview-context');
  await page.send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Escape', code: 'Escape' });
  await page.send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Escape', code: 'Escape' });

  for (const viewport of darkThemeViewports.filter((item) => item.id !== 'desktop')) {
    await setViewport(viewport.width, viewport.height);
    await navigate(`/#/sessions/${sessionId}`);
    await waitForText('追加描述');
    await sleep(750);
    let promptMenuVisible = false;
    for (let attempt = 0; attempt < 3 && !promptMenuVisible; attempt += 1) {
      await clickAria('运行参数');
      promptMenuVisible = await evaluate(`Array.from(document.querySelectorAll('.prompt-config-menu')).some((element) => {
        const rect = element.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0;
      })`);
    }
    assert(promptMenuVisible, `prompt config menu is not visible at ${viewport.id}`);
    await darkThemeAudit.capture('menu-prompt-config', viewport);
    await page.send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Escape', code: 'Escape' });
    await page.send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Escape', code: 'Escape' });
  }
  await setViewport(1440, 900);
  await navigate(`/#/sessions/${sessionId}`);
  await waitForText('追加描述');
  await clickAria('快捷回复');
  await auditDarkThemeSurface('menu-quick-reply');
}

async function screenshot(name, directory = screenshotDir) {
  const shot = await page.send('Page.captureScreenshot', {
    format: 'png',
    captureBeyondViewport: false,
  });
  await mkdir(directory, { recursive: true });
  await writeFile(`${directory}/${name}`, Buffer.from(shot.data, 'base64'));
}

async function assertDirectoryBrowser() {
  const data = await graphql(`
    query BrowseDirectory($input: BrowseDirectoryInput!) {
      browseDirectory(input: $input) {
        path
        entries { name path isDir canRead errorCode }
      }
    }
  `, { input: { path: '/workspaces' } });
  assert(data.browseDirectory.path === '/workspaces', 'browseDirectory did not return /workspaces');
}

async function ensureProject({ path, name, isGit }) {
  const existing = await graphql(`
    query Projects {
      projects {
        id
        name
        path
        isGit
        gitState { currentBranch branches { name isCurrent } }
      }
    }
  `);
  const current = existing.projects.find((project) => project.path === path);
  if (current) {
    assert(current.isGit === isGit, `existing project ${path} git state mismatch`);
    return current;
  }
  const data = await graphql(`
    mutation CreateProject($input: CreateProjectInput!) {
      createProject(input: $input) {
        id
        name
        path
        isGit
        gitState { currentBranch branches { name isCurrent } }
      }
    }
  `, { input: { path, name } });
  assert(data.createProject.isGit === isGit, `created project ${path} git state mismatch`);
  return data.createProject;
}

async function runPlainSession(projectId) {
  const session = await createSession({
    projectId,
    requirement: '请只输出 OK，然后结束。不要修改文件。',
    mode: 'chat',
    config: { permissionMode: 'workspace-write' },
  });
  await startSession(session.id);
  const finalStatus = await waitForSessionStatus(session.id, 240_000);
  assertTerminalStatus(finalStatus, 'plain session');
  await assertSessionEventsDoNotContain(session.id, 'bwrap');

  const diff = await sessionDiff(session.id, 'all');
  assert(diff.available === false, 'plain session diff should be unavailable');
  return { ...session, finalStatus };
}

async function runGitSession(projectId) {
  const session = await createSession({
    projectId,
    requirement: [
      '先使用 update_plan 建立两项 TODO，并把两项都标记为完成。',
      '请直接执行 shell 命令：',
      "`printf 'ANYCODE_GIT_E2E_OK\\n' > e2e-codex-output.txt`",
      '不要修改其他文件，然后结束。',
    ].join('\n'),
    mode: 'chat',
    baseBranch: 'main',
    config: { permissionMode: 'workspace-write' },
  });
  await startSession(session.id);
  const finalStatus = await waitForSessionStatus(session.id, 240_000);
  assertTerminalStatus(finalStatus, 'git session');
  await assertSessionEventsDoNotContain(session.id, 'bwrap');

  const diff = await sessionDiff(session.id, 'all');
  assert(diff.available === true, 'git session diff should be available');
  assert(diff.files.some((file) => file.path === 'e2e-codex-output.txt'), 'git diff missing e2e-codex-output.txt');
  const outputDiff = diff.allDiff.find((fileDiff) => fileDiff.file.path === 'e2e-codex-output.txt');
  assert(outputDiff, 'allDiff missing e2e-codex-output.txt');
  const diffText = outputDiff.hunks.flatMap((hunk) => hunk.lines.map((line) => line.content)).join('\n');
  assert(diffText.includes('ANYCODE_GIT_E2E_OK'), 'git diff missing expected marker');
  return { ...session, finalStatus };
}

async function assertWorkflowConfigInteractions(projectId) {
  await navigate(`/#/projects/${projectId}/workflow`);
  await waitForText('流程配置');
  const dropped = await evaluate(`(() => {
    const source = Array.from(document.querySelectorAll('.workflow-list .q-item'))
      .find((element) => element.innerText.includes('运行 Codex 节点'));
    const board = document.querySelector('.workflow-canvas-board');
    if (!source || !board) return false;
    const transfer = new DataTransfer();
    source.dispatchEvent(new DragEvent('dragstart', { bubbles: true, dataTransfer: transfer }));
    const bounds = board.getBoundingClientRect();
    board.dispatchEvent(new DragEvent('drop', {
      bubbles: true,
      cancelable: true,
      clientX: bounds.left + bounds.width / 2,
      clientY: bounds.top + bounds.height / 2,
      dataTransfer: transfer,
    }));
    return true;
  })()`);
  assert(dropped, 'workflow Codex node drag source or canvas is missing');
  await waitForText('节点 ID');
  await clickText('保存');
  await waitForWorkflowSaved(projectId);
  await assertNoHorizontalOverflow('workflow config interactions');
}

async function assertWorkflowCardFlow(projectId) {
  const definition = await saveWorkflowDefinition({
    projectId,
    name: `E2E approval flow ${stamp}`,
    graph: {
      nodes: [{
        id: 'approve',
        type: 'approval',
        title: '人工审批',
        prompt: '',
        position: { x: 80, y: 80 },
        approval: { beforeRun: true, afterRun: false },
        retry: { maxAttempts: 0 },
        merge: null,
      }],
      edges: [],
    },
  });
  await setDefaultWorkflow(projectId, definition.id);
  const session = await createSession({
    projectId,
    requirement: `E2E_APPROVAL_FLOW_${stamp}`,
    mode: 'workflow',
    baseBranch: 'main',
    config: { permissionMode: 'workspace-write' },
  });
  assert(session.status === 'waiting_approval', `workflow session status = ${session.status}, want waiting_approval`);

  await navigate(`/#/sessions/${session.id}`);
  await waitForText('E2E_APPROVAL_FLOW');
  await waitForText('待审批');
  await waitForText('等待人工审批');
  await assertNoHorizontalOverflow('workflow waiting approval detail');
  return session;
}

async function assertAnswerUserFlow(projectId) {
  const session = await createSession({
    projectId,
    requirement: [
      `E2E_WAITING_USER_CARD_${stamp}`,
      '请直接执行 shell 命令：sleep 45，然后结束。',
    ].join('\n'),
    mode: 'chat',
    config: { permissionMode: 'workspace-write' },
  });
  await startSession(session.id);
  await waitForSessionRunning(session.id, 20_000);
  const mcpPromise = callAnswerUser(session.id);
  const pending = await waitForPendingQuestion(session.id);

  await navigate(`/#/sessions/${session.id}`);
  await waitForText('E2E_WAITING_USER_CARD');
  await waitForText('待回答问题');
  await waitForText('Choose next step');
  await screenshot('12-answer-user-inline.png');
  if (darkThemeAudit) {
    await navigate('/');
    await waitForText('E2E_WAITING_USER_CARD');
    await clickQuestionButtonForCard('E2E_WAITING_USER_CARD');
    await waitForVisibleSelector('.answer-dialog');
    await auditDarkThemeSurface('dialog-answer-user', 'questions');
    await clickTab('Diff');
    await waitForVisibleSelector('.answer-dialog .diff-workspace');
    await auditDarkThemeSurface('dialog-answer-user', 'diff');
    await closeVisibleDialog();
  }
  await submitPendingQuestion(pending);
  const mcpResponse = await mcpPromise;
  assert(mcpResponse.status === 200, `answer_user MCP status = ${mcpResponse.status}: ${JSON.stringify(mcpResponse.body)}`);
  assert(JSON.stringify(mcpResponse.body).includes(pending.batchId), 'answer_user response missing batch id');
  await navigate(`/#/sessions/${session.id}`);
  await waitForText('E2E_WAITING_USER_CARD');
  await waitForSessionStatus(session.id, 10_000);
  await stopSessionBestEffort(session.id);
  return { id: session.id, batchId: pending.batchId };
}

async function createSession(input) {
  const data = await graphql(`
    mutation CreateSession($input: CreateSessionInput!) {
      createSession(input: $input) {
        id
        projectId
        status
        mode
        baseBranch
        worktreePath
      }
    }
  `, { input });
  assert(data.createSession.id, 'createSession missing id');
  return data.createSession;
}

async function startSession(id) {
  const data = await graphql(`
    mutation StartSession($id: ID!) {
      startSession(id: $id) {
        id
        status
        codexSessionId
      }
    }
  `, { id });
  assert(['queued', 'starting', 'running', 'stopped'].includes(data.startSession.status), `unexpected start status ${data.startSession.status}`);
  return data.startSession;
}

async function stopSessionBestEffort(id) {
  try {
    await graphql(`
      mutation StopSession($id: ID!) {
        stopSession(id: $id) { id status }
      }
    `, { id });
  } catch {
    // The session may have already reached a terminal state.
  }
}

async function saveWorkflowDefinition(input) {
  const data = await graphql(`
    mutation SaveWorkflowDefinition($input: SaveWorkflowDefinitionInput!) {
      saveWorkflowDefinition(input: $input) {
        id
        projectId
        name
        graph { nodes { id type title } }
      }
    }
  `, { input });
  assert(data.saveWorkflowDefinition.id, 'saveWorkflowDefinition missing id');
  return data.saveWorkflowDefinition;
}

async function setDefaultWorkflow(projectId, workflowId) {
  const data = await graphql(`
    mutation SetDefaultWorkflow($input: SetDefaultWorkflowInput!) {
      setDefaultWorkflow(input: $input) {
        id
        defaultWorkflowId
      }
    }
  `, { input: { projectId, workflowId } });
  assert(data.setDefaultWorkflow.defaultWorkflowId === workflowId, 'setDefaultWorkflow did not persist workflow id');
}

async function waitForWorkflowSaved(projectId, timeoutMs = 15_000) {
  const started = Date.now();
  while (Date.now() - started < timeoutMs) {
    const data = await graphql(`
      query Projects {
        projects {
          id
          defaultWorkflowId
        }
      }
    `);
    const project = data.projects.find((item) => item.id === projectId);
    const workflowId = project?.defaultWorkflowId || '';
    if (workflowId) {
      const definition = await graphql(`
        query WorkflowDefinition($id: ID!) {
          workflowDefinition(id: $id) {
            id
            graph { nodes { title } }
          }
        }
      `, { id: workflowId });
      const titles = definition.workflowDefinition?.graph?.nodes?.map((node) => node.title) || [];
      if (titles.includes('新节点')) return;
    }
    await sleep(250);
  }
  throw new Error(`Timed out waiting for workflow save for project ${projectId}`);
}

async function waitForSessionStatus(sessionId, timeoutMs) {
  const terminal = new Set(['stopped', 'failed', 'blocked', 'completed', 'closed', 'resume_failed']);
  const started = Date.now();
  let status = 'unknown';
  while (Date.now() - started < timeoutMs) {
    const data = await graphql(`
      query SessionStatus($id: ID!) {
        session(id: $id) {
          id
          status
        }
      }
    `, { id: sessionId });
    status = data.session.status;
    if (terminal.has(status)) return status;
    await sleep(1000);
  }
  return status;
}

async function waitForSessionRunning(sessionId, timeoutMs) {
  const terminal = new Set(['stopped', 'failed', 'blocked', 'completed', 'closed', 'resume_failed']);
  const started = Date.now();
  let status = 'unknown';
  while (Date.now() - started < timeoutMs) {
    const data = await graphql(`
      query SessionStatus($id: ID!) {
        session(id: $id) {
          id
          status
          codexSessionId
        }
      }
    `, { id: sessionId });
    status = data.session.status;
    if (status === 'running' && data.session.codexSessionId) return status;
    if (terminal.has(status)) throw new Error(`session ${sessionId} reached ${status} before answer_user test`);
    await sleep(500);
  }
  throw new Error(`Timed out waiting for session ${sessionId} to run; last status ${status}`);
}

async function waitForPendingQuestion(sessionId, timeoutMs = 10_000) {
  const started = Date.now();
  while (Date.now() - started < timeoutMs) {
    const data = await graphql(`
      query PendingQuestionBatches($sessionId: ID!) {
        pendingQuestionBatches(sessionId: $sessionId) {
          id
          status
          questions { id options { id label } }
        }
      }
    `, { sessionId });
    const batch = data.pendingQuestionBatches.find((item) => item.status === 'pending');
    const question = batch?.questions?.[0];
    if (batch?.id && question?.id) {
      return {
        batchId: batch.id,
        questionId: question.id,
        optionId: question.options?.[0]?.id || '',
      };
    }
    await sleep(250);
  }
  throw new Error(`Timed out waiting for pending question for ${sessionId}`);
}

async function submitPendingQuestion(pending) {
  assert(pending.batchId && pending.questionId && pending.optionId, `invalid pending question: ${JSON.stringify(pending)}`);
  const data = await graphql(`
    mutation SubmitQuestionBatch($input: SubmitQuestionBatchInput!) {
      submitQuestionBatch(input: $input) {
        id
        status
        questions { id selectedOptionId status }
      }
    }
  `, {
    input: {
      batchId: pending.batchId,
      answers: [{
        questionId: pending.questionId,
        selectedOptionId: pending.optionId,
      }],
    },
  });
  assert(data.submitQuestionBatch.status === 'answered', `submitQuestionBatch status = ${data.submitQuestionBatch.status}`);
}

function assertTerminalStatus(status, label) {
  assert(['stopped', 'completed'].includes(status), `${label} ended with ${status}`);
}

async function appendPrompt(sessionId) {
  const data = await graphql(`
    mutation AppendPrompt($input: AppendPromptInput!) {
      appendPrompt(input: $input) {
        id
        sessionId
        body
      }
    }
  `, { input: { sessionId, body: 'E2E 追加描述验证。' } });
  assert(data.appendPrompt.body.includes('E2E'), 'appendPrompt did not persist body');
}

async function sessionDiff(sessionId, mode) {
  return (await graphql(`
    query SessionDiff($input: SessionDiffInput!) {
      sessionDiff(input: $input) {
        mode
        available
        files { path status additions deletions }
        allDiff {
          file { path status additions deletions }
          hunks {
            header
            lines { kind content }
          }
        }
      }
    }
  `, { input: { sessionId, mode } })).sessionDiff;
}

async function assertSessionEventsDoNotContain(sessionId, text) {
  const data = await graphql(`
    query SessionTranscript($input: ListTranscriptEventsInput!) {
      sessionTranscript(input: $input) {
        events {
          content {
            __typename
            ... on TranscriptMessageContent { role text format }
            ... on TranscriptReasoningContent { text }
            ... on TranscriptCommandContent { commands { command workdir } output exitCode durationMs }
            ... on TranscriptToolContent { qualifiedName category input { format text } output { format text } }
            ... on TranscriptFileChangeContent { changes { kind path movePath unifiedDiff } }
            ... on TranscriptStatusContent { code level message details }
            ... on TranscriptUnknownContent { rawType payload }
          }
        }
      }
    }
  `, { input: { sessionId, limit: 100 } });
  const raw = JSON.stringify(data.sessionTranscript.events);
  assert(!raw.includes(text), `session ${sessionId} events still contain ${text}`);
}

async function callAnswerUser(sessionId) {
  return callSessionMCP(sessionId, 'answer_user', {
    questions: [{
      title: 'Choose next step',
      body: 'How should Codex continue?',
      type: 'choice',
      allowCustom: true,
      options: [{ id: 'continue', label: 'Continue', description: 'Proceed' }],
    }],
  });
}

async function callSessionMCP(sessionId, name, toolArguments) {
  const body = {
    jsonrpc: '2.0',
    id: 1,
    method: 'tools/call',
    params: {
      name,
      arguments: toolArguments,
    },
  };
  const response = await fetch(`${baseURL}/mcp/sessions/${sessionId}`, {
    method: 'POST',
    headers: {
      'content-type': 'application/json',
      authorization: `Bearer ${accessKey}`,
    },
    body: JSON.stringify(body),
  });
  const text = await response.text();
  let parsed;
  try {
    parsed = JSON.parse(text);
  } catch {
    parsed = { text };
  }
  return { status: response.status, body: parsed };
}

async function graphql(query, variables = {}) {
  const result = await evaluate(`fetch('/graphql', {
    method: 'POST',
    headers: {
      'content-type': 'application/json',
      'authorization': 'Bearer ${accessKey}',
    },
    body: ${JSON.stringify(JSON.stringify({ query, variables }))},
  }).then(async (res) => {
    const text = await res.text();
    let body;
    try {
      body = JSON.parse(text);
    } catch {
      body = { text };
    }
    return { status: res.status, body };
  })`);
  if (result.status < 200 || result.status >= 300 || result.body.errors) {
    throw new Error(`GraphQL failed: ${JSON.stringify(result, null, 2)}`);
  }
  return result.body.data;
}

function run(command, commandArgs, options = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, commandArgs, {
      cwd: process.cwd(),
      env: options.env || process.env,
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (chunk) => { stdout += chunk.toString(); });
    child.stderr.on('data', (chunk) => { stderr += chunk.toString(); });
    child.on('close', (code) => {
      if (code === 0 || options.allowFailure) {
        resolve({ code, stdout, stderr });
        return;
      }
      reject(new Error(`${command} ${commandArgs.join(' ')} failed with ${code}\n${stdout}\n${stderr}`));
    });
    child.on('error', reject);
  });
}

function shellQuote(value) {
  return `'${String(value).replaceAll("'", "'\"'\"'")}'`;
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}
