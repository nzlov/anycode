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
  await page.send('Page.enable');
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

  await navigate('/#/sessions');
  await waitForText('会话表格');
  await waitForText('请只输出 OK');
  await waitForText('ANYCODE_GIT_E2E_OK');
  await assertNoHorizontalOverflow('desktop sessions');
  await screenshot('02-sessions-desktop.png');

  await navigate(`/#/sessions/${plainSession.id}`);
  await waitForText('请只输出 OK');
  await waitForText('会话信息');
  await assertNoHorizontalOverflow('plain session detail');
  await screenshot('03-plain-session-detail.png');

  await navigate(`/#/sessions/${gitSession.id}`);
  await waitForText('当前变更');
  await clickText('当前变更');
  await waitForText('e2e-codex-output.txt');
  await assertNoHorizontalOverflow('git session detail');
  await screenshot('04-git-session-detail.png');

  await appendPrompt(gitSession.id);

  await navigate(`/#/diff?sessionId=${gitSession.id}&mode=all`);
  await waitForText('Diff');
  await waitForText('e2e-codex-output.txt');
  await assertNoHorizontalOverflow('git diff');
  await screenshot('05-git-diff-desktop.png');

  await navigate(`/#/projects/${gitProject.id}/workflow`);
  await waitForText('流程配置');
  await assertNoHorizontalOverflow('workflow');
  await screenshot('06-workflow-desktop.png');

  await setViewport(390, 844);
  await navigate('/');
  await waitForText('AnyCode');
  await assertNoHorizontalOverflow('mobile overview');
  await screenshot('07-overview-mobile.png');

  await navigate(`/#/sessions/${gitSession.id}`);
  await waitForText('当前变更');
  await assertNoHorizontalOverflow('mobile git session detail');
  await screenshot('08-git-session-mobile.png');

  browserState.assertClean();
  console.log(JSON.stringify({
    ok: true,
    plainProjectId: plainProject.id,
    plainSessionId: plainSession.id,
    plainFinalStatus: plainSession.finalStatus,
    gitProjectId: gitProject.id,
    gitSessionId: gitSession.id,
    gitFinalStatus: gitSession.finalStatus,
    gitDiffFile: 'e2e-codex-output.txt',
    screenshots: [
      '01-overview-desktop.png',
      '02-sessions-desktop.png',
      '03-plain-session-detail.png',
      '04-git-session-detail.png',
      '05-git-diff-desktop.png',
      '06-workflow-desktop.png',
      '07-overview-mobile.png',
      '08-git-session-mobile.png',
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
  throw new Error(`Timed out waiting for text ${text}. Body: ${body}`);
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

  const diff = await sessionDiff(session.id, 'all');
  assert(diff.available === true, 'git session diff should be available');
  assert(diff.files.items.some((file) => file.path === 'e2e-codex-output.txt'), 'git diff missing e2e-codex-output.txt');
  const outputDiff = diff.allDiff.find((fileDiff) => fileDiff.file.path === 'e2e-codex-output.txt');
  assert(outputDiff, 'allDiff missing e2e-codex-output.txt');
  const diffText = outputDiff.hunks.flatMap((hunk) => hunk.lines.map((line) => line.content)).join('\n');
  assert(diffText.includes('ANYCODE_GIT_E2E_OK'), 'git diff missing expected marker');
  return { ...session, finalStatus };
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
  assert(['starting', 'running', 'stopped'].includes(data.startSession.status), `unexpected start status ${data.startSession.status}`);
  return data.startSession;
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
