#!/usr/bin/env node
import { spawn } from 'node:child_process';
import { mkdir, rm, writeFile } from 'node:fs/promises';
import { homedir } from 'node:os';
import { setTimeout as sleep } from 'node:timers/promises';

const args = new Set(process.argv.slice(2));
const manageDocker = args.has('--manage-docker');
const accessKey = process.env.ANYCODE_ACCESS_KEY || 'test';
const baseURL = process.env.ANYCODE_E2E_BASE_URL || 'http://127.0.0.1:8080';
const codexHome = process.env.ANYCODE_CODEX_HOME || `${homedir()}/.codex`;
const stamp = `${Date.now()}-${Math.random().toString(16).slice(2, 8)}`;
const plainPath = process.env.ANYCODE_E2E_PLAIN_PATH || `/workspaces/e2e-anycode-plain-${stamp}`;
const gitPath = process.env.ANYCODE_E2E_GIT_PATH || `/workspaces/e2e-anycode-git-${stamp}`;
const debugPort = Number(process.env.ANYCODE_E2E_CDP_PORT || 9333);
const debugURL = `http://127.0.0.1:${debugPort}`;
const userDataDir = `/tmp/anycode-chromium-e2e-${stamp}`;
const screenshotDir = process.env.ANYCODE_E2E_SCREENSHOT_DIR || '/tmp/anycode-headless';
const overrideFile = `/tmp/anycode-codex-override-${stamp}.yml`;

let chrome;
let page;
let browserFailures;

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
    source: `localStorage.setItem('anycode.accessKey', ${JSON.stringify(accessKey)});`,
  });

  await setViewport(1440, 900);
  await navigate('/');
  await evaluate(`localStorage.setItem('anycode.accessKey', ${JSON.stringify(accessKey)});`);
  await navigate('/');
  browserState.clear();
  await waitForText('AnyCode');
  await waitForText('总揽');
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
  await waitForText('最新');
  await waitForText('历史');
  await waitForText('提交 ');
  await assertSessionCardDoesNotShowProject('ANYCODE_GIT_E2E_OK', gitProject.name);
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
  await screenshotFileDiffDialog('e2e-codex-output.txt');
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
  await waitForText('AnyCode');
  await assertNoHorizontalOverflow('mobile overview');
  await screenshot('08-overview-mobile.png');

  await navigate(`/#/sessions/${gitSession.id}`);
  await waitForText('Shell result');
  await assertSessionDetailReadableLayout('mobile git session detail layout');
  await assertNoHorizontalOverflow('mobile git session detail');
  await screenshot('09-git-session-mobile.png');

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
      '13-file-diff-dialog.png',
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

async function clickText(text) {
  const clicked = await evaluate(`(() => {
    const target = Array.from(document.querySelectorAll('button, [role="tab"], .q-tab, .q-item'))
      .find((element) => element.innerText && element.innerText.includes(${JSON.stringify(text)}));
    if (!target) return false;
    target.click();
    return true;
  })()`);
  assert(clicked, `click target not found: ${text}`);
  await sleep(300);
}

async function clickAria(label) {
  const clicked = await evaluate(`(() => {
    const target = Array.from(document.querySelectorAll('[aria-label=${JSON.stringify(label)}]'))
      .find((element) => {
        const rect = element.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0;
      });
    if (!target) return false;
    target.click();
    return true;
  })()`);
  assert(clicked, `aria target not found: ${label}`);
  await sleep(300);
}

async function closeVisibleDialog() {
  const closed = await evaluate(`(() => {
    const dialog = Array.from(document.querySelectorAll('.q-dialog')).find((element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0;
    });
    const target = dialog?.querySelector('[aria-label="关闭"], [aria-label="取消"]');
    if (!target) return false;
    target.click();
    return true;
  })()`);
  assert(closed, 'visible dialog close button not found');
  await sleep(300);
}

async function assertSessionCardDoesNotShowProject(marker, projectName) {
  const result = await evaluate(`(() => {
    const card = Array.from(document.querySelectorAll('.lane-session-card'))
      .find((element) => element.innerText.includes(${JSON.stringify(marker)}));
    return {
      found: Boolean(card),
      cardText: card?.innerText || '',
    };
  })()`);
  assert(result.found, `overview card not found for ${marker}`);
  assert(!result.cardText.includes(projectName), `card still shows project name ${projectName}: ${result.cardText}`);
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
  await waitForText('AnyCode');
  await clickAria('新建卡片');
  await waitForText('新建卡片');
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
  await closeVisibleDialog();
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
  await clickAria('选择项目目录');
  await waitForText('选择项目目录');
  await waitForText('当前路径');
  await screenshot('10-directory-dialog.png');
  await closeVisibleDialog();
}

async function screenshotFileDiffDialog(filePath) {
  const opened = await evaluate(`(() => {
    const panel = document.querySelector('.right-panel-card');
    const row = Array.from(panel?.querySelectorAll('.changes-list .q-item') || [])
      .find((element) => element.innerText && element.innerText.includes(${JSON.stringify(filePath)}));
    if (!row) return false;
    row.click();
    return true;
  })()`);
  assert(opened, `diff file row not found: ${filePath}`);
  await waitForVisibleSelector('.file-diff-dialog');
  await screenshot('13-file-diff-dialog.png');
  await closeVisibleDialog();
}

async function assertSessionDetailReadableLayout(label) {
  const result = await evaluate(`(() => {
    const doc = document.documentElement;
    const page = document.querySelector('.detail-page');
    const pageContainer = document.querySelector('.q-page-container');
    const eventTexts = Array.from(document.querySelectorAll('.event-message__body, .event-status__body, .event-tool__header span'))
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
      expandedToolBodies: document.querySelectorAll('.event-tool__body').length,
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

async function clickQuestionButtonForCard(marker) {
  const clicked = await evaluate(`(() => {
    const card = Array.from(document.querySelectorAll('.lane-session-card'))
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

async function screenshot(name) {
  const shot = await page.send('Page.captureScreenshot', {
    format: 'png',
    captureBeyondViewport: false,
  });
  await writeFile(`${screenshotDir}/${name}`, Buffer.from(shot.data, 'base64'));
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
  const finalStatus = await waitForSessionStatus(session.id, 120_000);
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
      '请直接执行 shell 命令：',
      "`printf 'ANYCODE_GIT_E2E_OK\\n' > e2e-codex-output.txt`",
      '不要修改其他文件，然后结束。',
    ].join('\n'),
    mode: 'chat',
    baseBranch: 'main',
    config: { permissionMode: 'workspace-write' },
  });
  await startSession(session.id);
  const finalStatus = await waitForSessionStatus(session.id, 120_000);
  assertTerminalStatus(finalStatus, 'git session');
  await assertSessionEventsDoNotContain(session.id, 'bwrap');

  const diff = await sessionDiff(session.id, 'all');
  assert(diff.available === true, 'git session diff should be available');
  assert(diff.files.items.some((file) => file.path === 'e2e-codex-output.txt'), 'git diff missing e2e-codex-output.txt');
  const outputDiff = diff.allDiff.find((fileDiff) => fileDiff.file.path === 'e2e-codex-output.txt');
  assert(outputDiff, 'allDiff missing e2e-codex-output.txt');
  const diffText = outputDiff.hunks.flatMap((hunk) => hunk.lines.map((line) => line.content)).join('\n');
  assert(diffText.includes('ANYCODE_GIT_E2E_OK'), 'git diff missing expected marker');
  return { ...session, finalStatus };
}

async function assertWorkflowConfigInteractions(projectId) {
  await navigate(`/#/projects/${projectId}/workflow`);
  await waitForText('流程配置');
  await clickAria('新增节点');
  await waitForText('新节点');
  await clickText('应用节点');
  await clickText('保存为默认流程');
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
        }
      }
    `, { id: sessionId });
    status = data.session.status;
    if (status === 'running' || status === 'starting') return status;
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
        files {
          items { path status additions deletions }
          pageInfo { page pageSize total nextCursor }
        }
        allDiff {
          file { path status additions deletions }
          hunks {
            header
            lines { kind content }
          }
        }
      }
    }
  `, { input: { sessionId, mode, page: 1, pageSize: 50 } })).sessionDiff;
}

async function assertSessionEventsDoNotContain(sessionId, text) {
  const data = await graphql(`
    query SessionEvents($input: ListSessionEventsInput!) {
      sessionEvents(input: $input) {
        items {
          type
          payload
        }
      }
    }
  `, { input: { sessionId, page: 1, pageSize: 100 } });
  const raw = JSON.stringify(data.sessionEvents.items);
  assert(!raw.includes(text), `session ${sessionId} events still contain ${text}`);
}

async function callAnswerUser(sessionId) {
  const body = {
    jsonrpc: '2.0',
    id: 1,
    method: 'tools/call',
    params: {
      name: 'answer_user',
      arguments: {
        questions: [{
          title: 'Choose next step',
          body: 'How should Codex continue?',
          type: 'choice',
          allowCustom: true,
          options: [{ id: 'continue', label: 'Continue', description: 'Proceed' }],
        }],
      },
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
