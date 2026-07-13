#!/usr/bin/env node
import { spawn, spawnSync } from 'node:child_process';
import { mkdir, rm, writeFile } from 'node:fs/promises';
import { homedir } from 'node:os';
import { setTimeout as sleep } from 'node:timers/promises';

const accessKey = process.env.ANYCODE_ACCESS_KEY || 'anycode-dev';
const baseURL = process.env.ANYCODE_E2E_BASE_URL || 'http://127.0.0.1:8080';
const debugPort = Number(process.env.ANYCODE_E2E_CDP_PORT || 9334);
const debugURL = `http://127.0.0.1:${debugPort}`;
const stamp = `${Date.now()}-${Math.random().toString(16).slice(2, 8)}`;
const userDataDir = `/tmp/anycode-chromium-click-audit-${stamp}`;
const screenshotDir = process.env.ANYCODE_CLICK_AUDIT_SCREENSHOT_DIR || '/tmp/anycode-click-audit';

let chrome;
let page;
const results = [];
let createdOverviewMarker = '';

try {
  await waitForHTTPHealth();
  await rm(userDataDir, { recursive: true, force: true }).catch(() => {});
  await rm(screenshotDir, { recursive: true, force: true });
  await mkdir(screenshotDir, { recursive: true });

  const auditData = await loadAuditData();
  chrome = launchChromium();
  await waitForChrome();
  page = await connectPage();
  const browserState = trackBrowserFailures(page);

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
  const directoryProjectName = ensureDirectoryAuditWorkspace();

  await test('移动端顶部导航菜单按钮可打开和关闭侧栏', async () => {
    await setViewport(390, 844);
    await navigate('/');
    await waitForText('AnyCode');
    await clickAria('打开导航');
    await waitForCondition(`Array.from(document.querySelectorAll(".app-drawer")).some((element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    })`, 'drawer opened');
    await clickAria('打开导航');
    await waitForCondition(`!Array.from(document.querySelectorAll(".app-drawer")).some((element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    })`, 'drawer closed');
    await setViewport(1440, 900);
    await navigate('/');
    await waitForText('AnyCode');
  });

  await test('顶部目录弹窗关闭按钮生效', async () => {
    await clickAria('选择项目目录');
    await waitForText('选择项目目录');
    await screenshot('01-directory-dialog.png');
    await closeVisibleDialog();
    await waitForNoDialog();
  });

  await test('目录弹窗打开路径、上一级、选择目录和打开项目按钮生效', async () => {
    await clickAria('选择项目目录');
    await waitForText('选择项目目录');
    await setInput('.directory-dialog input', '/workspaces');
    await clickAria('打开路径');
    await waitForText(directoryProjectName);
    await clickAria('上一级');
    await waitForText('/workspaces');
    await setInput('.directory-dialog input', '/workspaces');
    await clickAria('打开路径');
    await clickDirectorySelect(directoryProjectName);
    await clickText('打开该项目');
    await waitForNoDialog();
  });

  await test('目录弹窗取消按钮生效', async () => {
    await clickAria('选择项目目录');
    await waitForText('选择项目目录');
    await clickAria('取消');
    await waitForNoDialog();
  });

  await test('主题按钮可切换系统、深色和浅色', async () => {
    await clickAria('主题模式');
    await clickText('跟随系统');
    await waitForCondition('document.documentElement.dataset.themeMode === "system"', 'theme mode system');
    await clickAria('主题模式');
    await clickText('深色');
    await waitForCondition('document.documentElement.dataset.themeMode === "dark"', 'theme mode dark');
    await clickAria('主题模式');
    await clickText('浅色');
    await waitForCondition('document.documentElement.dataset.themeMode === "light"', 'theme mode light');
  });

  await test('访问密钥弹窗可显示密钥并保存', async () => {
    await clickAccessKeyItem();
    await waitForText('访问密钥');
    await clickAria('切换密钥可见性');
    await setInput('.access-key-dialog input', accessKey);
    await clickText('保存');
    await waitForText('已保存');
    await closeVisibleDialog();
  });

  await test('访问密钥弹窗取消按钮生效', async () => {
    await clickAccessKeyItem();
    await waitForText('访问密钥');
    await clickAria('取消');
    await waitForNoDialog();
  });

  await test('FAB 新建卡片可打开弹窗、切换模式和关闭', async () => {
    await clickAria('新建卡片');
    await waitForText('新建卡片');
    await clickText('会话模式');
    await clickText('流程模式');
    await screenshot('02-new-session-dialog.png');
    await closeVisibleDialog();
  });

  await test('新建卡片附件预览、删除和创建按钮生效', async () => {
    const marker = `CLICK_AUDIT_CREATE_${stamp}`;
    createdOverviewMarker = marker;
    await clickAria('新建卡片');
    await waitForText('新建卡片');
    await attachNewSessionFixtureFiles();
    await waitForText('click-audit.png');
    await clickText('click-audit.png');
    await waitForVisibleSelector('.attachment-preview-card');
    await clickAria('关闭预览');
    await removeAttachmentChip('click-audit.md');
    await waitForCondition(`!document.body.innerText.includes('click-audit.md')`, 'attachment removed');
    await setInput('.new-session-dialog textarea', marker);
    await clickText('创建卡片');
    await waitForNoDialog(15_000);
    await navigate('/');
    await waitForText(marker);
  });

  await test('项目列表可切换项目视图且非总览页不显示 FAB', async () => {
    await clickProjectItem(auditData.project.name);
    await waitForRouteIncludes(`projectId=${auditData.project.id}`);
    await waitForText(auditData.project.name);
    await navigate(`/#/projects/${auditData.project.id}/workflow`);
    await waitForText('流程配置');
    await waitForCondition(`!document.querySelector('[aria-label="新建卡片"]')`, 'FAB hidden outside overview');
  });

  await test('新建卡片切换项目后分支、权限、模型和思考强度可变更', async () => {
    await navigate('/');
    await clickAria('新建卡片');
    await waitForText('新建卡片');
    await selectNewSessionProject(auditData.project.name);
    await waitForText(auditData.project.defaultBranch || 'main');
    await selectComposerOption('.new-session-dialog', '完全访问');
    await selectComposerOption('.new-session-dialog', 'gpt-5.5');
    await selectComposerOption('.new-session-dialog', 'high');
    await waitForText('完全访问');
    await waitForText('gpt-5.5');
    await waitForText('high');
    await closeVisibleDialog();
  });

  await test('新建卡片选择非 git 项目时不显示基础分支', async () => {
    await navigate('/');
    await clickAria('新建卡片');
    await waitForText('新建卡片');
    await selectNewSessionProject(directoryProjectName);
    await waitForCondition(`!Array.from(document.querySelectorAll('.new-session-dialog .q-field'))
      .some((element) => element.innerText.includes('基础分支'))`, 'base branch hidden for non-git project');
    await closeVisibleDialog();
  });

  await test('项目设置菜单可跳转流程配置', async () => {
    await clickProjectSettings(auditData.project.name);
    await waitForCondition(`!document.body.innerText.includes('选择目录')`, 'project menu does not include directory selection');
    await clickText('流程配置');
    await waitForRouteIncludes(`/projects/${auditData.project.id}/workflow`);
    await waitForText('流程配置');
  });

  await test('流程配置新增、应用、删除、保存按钮生效', async () => {
    await navigate(`/#/projects/${auditData.project.id}/workflow`);
    await waitForText('流程配置');
    const before = await elementCount('.workflow-node');
    await clickAria('新增节点');
    await waitForCondition(`document.querySelectorAll('.workflow-node').length === ${before + 1}`, 'workflow node added');
    await clickText('应用节点');
    await clickAria('删除节点');
    await waitForCondition(`document.querySelectorAll('.workflow-node').length === ${before}`, 'workflow node deleted');
    await clickText('保存为默认流程');
    await sleep(500);
    await screenshot('03-workflow-buttons.png');
  });

  await test('流程配置节点可拖动且端口可连接节点', async () => {
    await navigate(`/#/projects/${auditData.project.id}/workflow`);
    await waitForText('流程配置');
    await clickAria('新增节点');
    await waitForCondition(`document.querySelectorAll('.workflow-node').length >= 2`, 'workflow has two nodes');
    const before = await firstWorkflowNodeTransform();
    await dragFirstWorkflowNode();
    await waitForCondition(`${JSON.stringify(before)} !== Array.from(document.querySelectorAll('.workflow-node'))[0]?.style.transform`, 'workflow node moved');
    await connectFirstTwoWorkflowNodes();
    await waitForCondition(`document.querySelectorAll('.workflow-edge').length >= 1`, 'workflow edge connected');
    await screenshot('03b-workflow-drag-connect.png');
  });

  await test('侧栏总揽和项目焦点唯一且不显示会话列表入口', async () => {
    await clickText('总揽');
    await waitForRouteIncludes('/#/');
    await waitForCondition(`!document.querySelector('.app-drawer')?.innerText.includes('会话表格')`, 'session list hidden from sidebar');
    await clickProjectItem(auditData.project.name);
    await waitForRouteIncludes(`projectId=${auditData.project.id}`);
    await waitForCondition(`Array.from(document.querySelectorAll('.app-drawer .q-item.q-item--active')).length === 1`, 'single active sidebar item');
    await navigate('/');
    await waitForCondition(`location.href.endsWith('/#/') || location.href.endsWith('/#')`, 'overview focus restored');
    await waitForCondition(`Array.from(document.querySelectorAll('.app-drawer .q-item.q-item--active')).length === 1`, 'single overview active item');
  });

  await test('会话表格过滤、打开卡片和查看 Diff 按钮生效', async () => {
    await navigate('/#/sessions');
    await waitForText('会话表格');
    await setInput('input[placeholder="搜索需求、项目或分支"]', auditData.marker);
    await waitForText(auditData.marker);
    await clickRowButton(auditData.marker, '查看 Diff');
    await waitForRouteIncludes('/diff');
    await waitForText('当前分支变更');
    await waitForText(auditData.diffFile);
    await clickText('单个文件');
    await clickText(auditData.diffFile);
    await waitForRouteIncludes('mode=single');
    await clickText('全部 Diff');
    await waitForRouteIncludes('mode=all');
    await clickDiffRefreshButton();
    await screenshot('04-diff-buttons.png');
  });

  await test('会话表格打开卡片按钮可进入详情', async () => {
    await navigate('/#/sessions');
    await waitForText('会话表格');
    await setInput('input[placeholder="搜索需求、项目或分支"]', auditData.marker);
    await waitForText(auditData.marker);
    await clickRowButton(auditData.marker, '打开卡片');
    await waitForRouteIncludes(`/sessions/${auditData.session.id}`);
    await waitForText('会话事件流');
  });

  await test('详情页当前变更、单文件 Diff 弹窗和完整 Diff 按钮生效', async () => {
    await navigate(`/#/sessions/${auditData.session.id}`);
    await waitForText('会话事件流');
    await clickText('当前变更');
    await waitForText(auditData.diffFile);
    await clickDetailChangesRefreshButton();
    await clickText(auditData.diffFile);
    await waitForVisibleSelector('.file-diff-dialog');
    await closeVisibleDialog();
    await clickText('查看全部');
    await waitForRouteIncludes('/diff');
    await navigate(`/#/sessions/${auditData.session.id}`);
    await waitForText('会话事件流');
    await clickText('完整 Diff');
    await waitForRouteIncludes('/diff');
    await waitForRouteIncludes(auditData.session.id);
  });

  await test('详情页追加描述发送按钮生效', async () => {
    const text = `CLICK_AUDIT_APPEND_${stamp}`;
    await navigate(`/#/sessions/${auditData.session.id}`);
    await waitForText('会话事件流');
    await setInput('.detail-composer textarea', text);
    await clickAria('发送追加描述');
    await waitForPromptAppend(auditData.session.id, text);
  });

  await test('详情页输入区附件、模型、思考强度和权限控件可操作', async () => {
    await navigate(`/#/sessions/${auditData.session.id}`);
    await waitForText('会话事件流');
    await attachDetailComposerFixtureFile();
    await waitForText('detail-audit.png');
    await clickText('detail-audit.png');
    await waitForVisibleSelector('.attachment-preview-card');
    await clickAria('关闭预览');
    await selectComposerOption('.detail-composer', 'gpt-5.5');
    await selectComposerOption('.detail-composer', 'high');
    await selectComposerOption('.detail-composer', '只读');
    await waitForText('gpt-5.5');
    await waitForText('high');
    await waitForText('只读');
  });

  await test('详情页运行和顶部停止按钮生效', async () => {
    const session = await createSession({
      projectId: auditData.project.id,
      requirement: `CLICK_AUDIT_RUN_STOP_${stamp}\n请直接执行 shell 命令：sleep 45，然后结束。`,
      mode: 'chat',
      baseBranch: 'main',
      config: { permissionMode: 'workspace-write' },
    });
    await navigate(`/#/sessions/${session.id}`);
    await waitForText('CLICK_AUDIT_RUN_STOP');
    await clickText('运行');
    await waitForSessionStatus(session.id, new Set(['starting', 'running']), 20_000);
    await clickText('停止');
    await waitForSessionStatus(session.id, new Set(['stopped', 'failed', 'completed']), 30_000);
  });

  await test('详情页输入框停止按钮生效', async () => {
    const session = await createSession({
      projectId: auditData.project.id,
      requirement: `CLICK_AUDIT_COMPOSER_STOP_${stamp}\n请直接执行 shell 命令：sleep 45，然后结束。`,
      mode: 'chat',
      baseBranch: 'main',
      config: { permissionMode: 'workspace-write' },
    });
    await navigate(`/#/sessions/${session.id}`);
    await waitForText('CLICK_AUDIT_COMPOSER_STOP');
    await clickText('运行');
    await waitForSessionStatus(session.id, new Set(['starting', 'running']), 20_000);
    await clickAria('停止会话');
    await waitForSessionStatus(session.id, new Set(['stopped', 'failed', 'completed']), 30_000);
  });

  await test('详情页关闭按钮生效', async () => {
    const session = await createSession({
      projectId: auditData.project.id,
      requirement: `CLICK_AUDIT_CLOSE_${stamp}`,
      mode: 'chat',
      baseBranch: 'main',
      config: { permissionMode: 'workspace-write' },
    });
    await navigate(`/#/sessions/${session.id}`);
    await waitForText('CLICK_AUDIT_CLOSE');
    await clickText('关闭');
    await waitForSessionStatus(session.id, new Set(['closed']), 20_000);
    await waitForText('已关闭');
  });

  await test('总揽泳道收起展开、统计链接、图标按钮和卡片点击可跳转', async () => {
    await navigate('/');
    await waitForText('总揽');
    await ensureLaneAtIndexExpanded(0);
    await toggleLaneAtIndex(0);
    await waitForLaneAtIndexExpanded(0, false);
    await toggleLaneAtIndex(0);
    await waitForLaneAtIndexExpanded(0, true);
    const marker = auditData.marker;
    await waitForText(marker);
    await clickLaneStat(marker, '提交');
    await waitForRouteIncludes('/commits');
    await waitForText('提交记录');
    await navigate('/');
    await waitForText(marker);
    await clickLaneStat(marker, '未提交');
    await waitForRouteIncludes('/diff');
    await waitForRouteIncludes(`projectId=${auditData.project.id}`);
    await waitForRouteIncludes('branch=');
    await navigate('/');
    await waitForText(marker);
    await ensureLaneExpanded(marker);
    await clickLaneOpenButton(marker);
    await waitForRouteIncludes('/sessions/');
    await navigate('/');
    await waitForText(marker);
    await ensureLaneExpanded(marker);
    await clickCard(marker);
    await waitForRouteIncludes('/sessions/');
    await screenshot('05-overview-card-click.png');
  });

  browserState.assertClean();
} catch (error) {
  results.push({ name: 'fatal', ok: false, error: errorMessage(error) });
  process.exitCode = 1;
} finally {
  if (page) page.close();
  await stopChromium();
  const failed = results.filter((item) => !item.ok);
  const report = {
    ok: failed.length === 0,
    baseURL,
    screenshotDir,
    passed: results.filter((item) => item.ok).length,
    failed: failed.length,
    results,
  };
  console.log(JSON.stringify(report, null, 2));
  if (failed.length > 0) process.exitCode = 1;
}

async function test(name, fn) {
  try {
    await resetDesktopOverview();
    await fn();
    results.push({ name, ok: true, route: await currentRoute() });
  } catch (error) {
    results.push({ name, ok: false, route: await currentRoute().catch(() => ''), error: errorMessage(error) });
  } finally {
    await closeAllDialogs().catch(() => {});
    await setViewport(1440, 900).catch(() => {});
  }
}

async function loadAuditData() {
  const projectsData = await graphql(`
    query Projects {
      projects {
        id
        name
        path
        isGit
        gitState {
          currentBranch
          branches { name isCurrent }
        }
      }
    }
  `);
  const gitProjects = projectsData.projects.filter((project) => project.isGit);
  assert(gitProjects.length > 0, 'No git project found. Run scripts/run-headless-e2e.sh first.');
  const preferredProject = gitProjects.find((project) => project.name.includes('git')) || gitProjects[0];
  const project = normalizeProject(preferredProject);
  const session = await createDiffAuditSession(project);
  const diff = await sessionDiff(session.id);
  assert(diff.available && diff.files.items.length > 0, 'Fresh git audit session has no available diff.');
  return {
    project,
    session,
    marker: session.requirementSummary,
    diffFile: diff.files.items[0].path,
  };
}

async function createDiffAuditSession(project) {
  const marker = `ANYCODE_CLICK_DIFF_${stamp}`;
  const session = await createSession({
    projectId: project.id,
    requirement: marker,
    mode: 'chat',
    baseBranch: project.defaultBranch || 'main',
    config: { permissionMode: 'workspace-write' },
  });
  const detailData = await graphql(`
    query Session($id: ID!) {
      session(id: $id) {
        id
        projectId
        baseBranch
        status
        worktreePath
        updatedAt
      }
    }
  `, { id: session.id });
  const detail = detailData.session;
  assert(detail.worktreePath, 'Fresh audit session did not create a worktree.');
  writeContainerFile(`${detail.worktreePath}/click-audit-${stamp}.txt`, `click audit diff ${stamp}\n`);
  await startSessionRequest(detail.id);
  const status = await waitForSessionStatus(detail.id, new Set(['starting', 'running', 'stopped', 'failed', 'completed']), 20_000);
  if (status === 'starting' || status === 'running') {
    await stopSessionRequest(detail.id);
    await waitForSessionStatus(detail.id, new Set(['stopped', 'failed', 'completed']), 30_000);
  }
  return { ...detail, projectName: project.name, requirementSummary: marker };
}

function normalizeProject(project) {
  const defaultBranch =
    project.gitState?.currentBranch ||
    project.gitState?.branches?.find((branch) => branch.isCurrent)?.name ||
    project.gitState?.branches?.[0]?.name ||
    'main';
  return { ...project, defaultBranch };
}

function ensureDirectoryAuditWorkspace() {
  const name = `click-audit-project-${stamp}`;
  const result = spawnSync('docker', [
    'compose',
    '-f',
    'compose.yml',
    'exec',
    '-T',
    'anycode',
    'mkdir',
    '-p',
    `/workspaces/${name}`,
  ], {
    cwd: process.cwd(),
    env: { ...process.env, ANYCODE_ACCESS_KEY: accessKey },
    encoding: 'utf8',
  });
  if (result.status !== 0) {
    throw new Error(`create click audit workspace failed: ${result.stderr || result.stdout}`);
  }
  return name;
}

function writeContainerFile(path, body) {
  const result = spawnSync('docker', [
    'compose',
    '-f',
    'compose.yml',
    'exec',
    '-T',
    'anycode',
    'sh',
    '-c',
    'cat > "$1"',
    'sh',
    path,
  ], {
    cwd: process.cwd(),
    env: { ...process.env, ANYCODE_ACCESS_KEY: accessKey },
    input: body,
    encoding: 'utf8',
  });
  if (result.status !== 0) {
    throw new Error(`write container file failed: ${result.stderr || result.stdout}`);
  }
}

async function sessionDiff(sessionId) {
  const data = await graphql(`
    query SessionDiff($input: SessionDiffInput!) {
      sessionDiff(input: $input) {
        available
        files { items { path } }
      }
    }
  `, { input: { sessionId, mode: 'all', page: 1, pageSize: 20 } });
  return data.sessionDiff;
}

async function createSession(input) {
  const data = await graphql(`
    mutation CreateSession($input: CreateSessionInput!) {
      createSession(input: $input) {
        id
        status
      }
    }
  `, { input });
  return data.createSession;
}

async function startSessionRequest(sessionId) {
  await graphql(`
    mutation StartSession($id: ID!) {
      startSession(id: $id) {
        id
        status
      }
    }
  `, { id: sessionId });
}

async function stopSessionRequest(sessionId) {
  await graphql(`
    mutation StopSession($id: ID!) {
      stopSession(id: $id) {
        id
        status
      }
    }
  `, { id: sessionId });
}

async function waitForSessionStatus(sessionId, accepted, timeoutMs) {
  const started = Date.now();
  let status = '';
  while (Date.now() - started < timeoutMs) {
    const data = await graphql(`
      query Session($id: ID!) {
        session(id: $id) {
          id
          status
        }
      }
    `, { id: sessionId });
    status = data.session.status;
    if (accepted.has(status)) return status;
    await sleep(500);
  }
  throw new Error(`Timed out waiting for session ${sessionId}; last status ${status}`);
}

async function waitForPromptAppend(sessionId, text, timeoutMs = 15_000) {
  const started = Date.now();
  while (Date.now() - started < timeoutMs) {
    const data = await graphql(`
      query Session($id: ID!) {
        session(id: $id) {
          id
          promptAppends {
            body
          }
        }
      }
    `, { id: sessionId });
    if (data.session.promptAppends.some((item) => item.body.includes(text))) return;
    await sleep(500);
  }
  throw new Error(`Timed out waiting for prompt append ${text}`);
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
  const state = { consoleErrors: [], pageErrors: [], failedRequests: [] };
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

async function setViewport(width, height) {
  await page.send('Emulation.setDeviceMetricsOverride', {
    width,
    height,
    deviceScaleFactor: 1,
    mobile: width < 600,
  });
}

async function resetDesktopOverview() {
  await setViewport(1440, 900);
  await navigate('/');
  await waitForText('AnyCode');
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

async function waitForText(text, timeoutMs = 20_000) {
  const escaped = JSON.stringify(text);
  const started = Date.now();
  while (Date.now() - started < timeoutMs) {
    const found = await evaluate(`document.body && document.body.innerText.includes(${escaped})`);
    if (found) return;
    await sleep(250);
  }
  throw new Error(`Timed out waiting for text ${text}`);
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

async function isVisible(selector) {
  return evaluate(`Array.from(document.querySelectorAll(${JSON.stringify(selector)})).some((element) => {
    const rect = element.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
  })`);
}

async function waitForCondition(expression, label, timeoutMs = 10_000) {
  const started = Date.now();
  while (Date.now() - started < timeoutMs) {
    if (await evaluate(expression)) return;
    await sleep(250);
  }
  throw new Error(`Timed out waiting for ${label}`);
}

async function waitForRouteIncludes(fragment, timeoutMs = 10_000) {
  const started = Date.now();
  while (Date.now() - started < timeoutMs) {
    const route = await currentRoute();
    if (route.includes(fragment)) return;
    await sleep(250);
  }
  throw new Error(`Timed out waiting for route to include ${fragment}; current ${await currentRoute()}`);
}

async function currentRoute() {
  return evaluate('location.href');
}

async function clickAria(label) {
  const clicked = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    };
    const target = Array.from(document.querySelectorAll('[aria-label=${JSON.stringify(label)}]')).find(visible);
    if (!target) return false;
    target.click();
    return true;
  })()`);
  assert(clicked, `aria target not found: ${label}`);
  await sleep(300);
}

async function clickText(text) {
  const clicked = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    };
    const target = Array.from(document.querySelectorAll('button, a, [role="button"], [role="tab"], .q-btn, .q-tab, .q-item, .q-chip'))
      .find((element) => visible(element) && element.innerText && element.innerText.includes(${JSON.stringify(text)}));
    if (!target) return false;
    target.click();
    return true;
  })()`);
  assert(clicked, `click target not found: ${text}`);
  await sleep(400);
}

async function clickRowButton(marker, ariaLabel) {
  const clicked = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    };
    const row = Array.from(document.querySelectorAll('tbody tr, .q-table__grid-content .q-card, .q-item'))
      .find((element) => visible(element) && element.innerText && element.innerText.includes(${JSON.stringify(marker)}));
    const button = row?.querySelector('[aria-label=${JSON.stringify(ariaLabel)}]');
    if (!button || !visible(button)) return false;
    button.click();
    return true;
  })()`);
  assert(clicked, `row button not found: ${marker} / ${ariaLabel}`);
  await sleep(500);
}

async function clickDirectorySelect(directoryName) {
  const clicked = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    };
    const scroll = document.querySelector('.directory-list');
    const item = Array.from(document.querySelectorAll('.directory-dialog .q-item'))
      .find((element) => element.innerText.includes(${JSON.stringify(directoryName)}));
    if (scroll && item) scroll.scrollTop = Math.max(0, item.offsetTop - 80);
    const button = item?.querySelector('[aria-label="选择目录"]');
    if (!button || !visible(button)) return false;
    button.click();
    return true;
  })()`);
  assert(clicked, `directory select button not found: ${directoryName}`);
  await sleep(400);
}

async function clickDiffRefreshButton() {
  const clicked = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    };
    const button = Array.from(document.querySelectorAll('.diff-files .files-header button')).find(visible);
    if (!button) return false;
    button.click();
    return true;
  })()`);
  assert(clicked, 'diff refresh button not found');
  await sleep(500);
}

async function clickDetailChangesRefreshButton() {
  const clicked = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    };
    const button = Array.from(document.querySelectorAll('.changes-header button')).find(visible);
    if (!button) return false;
    button.click();
    return true;
  })()`);
  assert(clicked, 'detail changes refresh button not found');
  await sleep(500);
}

async function clickProjectItem(projectName) {
  const clicked = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    };
    const scroll = document.querySelector('.app-drawer .q-scrollarea__container');
    const item = Array.from(document.querySelectorAll('.app-drawer .q-item'))
      .find((element) => element.innerText && element.innerText.includes(${JSON.stringify(projectName)}));
    if (scroll && item) scroll.scrollTop = Math.max(0, item.offsetTop - 120);
    if (!item || !visible(item)) return false;
    item.click();
    return true;
  })()`);
  assert(clicked, `project item not found: ${projectName}`);
  await sleep(500);
}

async function clickProjectSettings(projectName) {
  const clicked = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    };
    const scroll = document.querySelector('.app-drawer .q-scrollarea__container');
    const items = Array.from(document.querySelectorAll('.app-drawer .q-item'));
    const item = items.find((element) => element.innerText && element.innerText.includes(${JSON.stringify(projectName)}));
    if (scroll && item) scroll.scrollTop = Math.max(0, item.offsetTop - 120);
    const button = item?.querySelector('[aria-label="项目设置"]');
    if (!button || !visible(button)) return false;
    button.click();
    return true;
  })()`);
  assert(clicked, `project settings not found: ${projectName}`);
  await sleep(400);
}

async function selectNewSessionProject(projectName) {
  const opened = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    };
    const field = Array.from(document.querySelectorAll('.new-session-grid .q-field')).find(visible);
    if (!field) return false;
    field.click();
    return true;
  })()`);
  assert(opened, 'new session project select not found');
  await sleep(300);
  const selected = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    };
    const items = Array.from(document.querySelectorAll('.q-menu .q-item, [role="option"]')).filter(visible);
    const target = items.find((element) => element.innerText.includes(${JSON.stringify(projectName)})) || items.at(-1);
    if (!target) return false;
    target.click();
    return true;
  })()`);
  assert(selected, `new session project option not found: ${projectName}`);
  await sleep(500);
}

async function selectComposerOption(scopeSelector, optionText) {
  const opened = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    };
    const scope = document.querySelector(${JSON.stringify(scopeSelector)});
    if (!scope) return false;
    const fields = Array.from(scope.querySelectorAll('.compact-select'));
    const option = ${JSON.stringify(optionText)};
    const field = fields.find((element) => {
      const text = element.innerText || '';
      if (!visible(element)) return false;
      if (option.includes('gpt-')) return text.includes('Codex') || text.includes('gpt-');
      if (option.includes('思考')) return text.includes('思考');
      return text.includes('只读') || text.includes('工作区') || text.includes('完全访问');
    });
    if (!field) return false;
    field.click();
    return true;
  })()`);
  assert(opened, `composer select not found for ${optionText}`);
  await sleep(300);
  await clickText(optionText);
}

async function firstWorkflowNodeTransform() {
  return evaluate(`Array.from(document.querySelectorAll('.workflow-node'))[0]?.style.transform || ''`);
}

async function dragFirstWorkflowNode() {
  const rect = await evaluate(`(() => {
    const element = document.querySelector('.workflow-node');
    if (!element) return null;
    const rect = element.getBoundingClientRect();
    return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 };
  })()`);
  assert(rect, 'workflow node not found for drag');
  await page.send('Input.dispatchMouseEvent', { type: 'mousePressed', x: rect.x, y: rect.y, button: 'left', clickCount: 1 });
  await page.send('Input.dispatchMouseEvent', { type: 'mouseMoved', x: rect.x + 80, y: rect.y + 42, button: 'left' });
  await page.send('Input.dispatchMouseEvent', { type: 'mouseReleased', x: rect.x + 80, y: rect.y + 42, button: 'left', clickCount: 1 });
  await sleep(500);
}

async function connectFirstTwoWorkflowNodes() {
  const connected = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    };
    const nodes = Array.from(document.querySelectorAll('.workflow-node')).filter(visible);
    const output = nodes[0]?.querySelector('.workflow-port--output');
    const input = nodes[1]?.querySelector('.workflow-port--input');
    if (!output || !input) return false;
    output.click();
    input.click();
    return true;
  })()`);
  assert(connected, 'workflow ports not found for connect');
  await sleep(500);
}

async function clickAccessKeyItem() {
  const clicked = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    };
    const scroll = document.querySelector('.app-drawer .q-scrollarea__container');
    const items = Array.from(document.querySelectorAll('.app-drawer .q-item'));
    const item = items
      .find((element) => visible(element) && element.innerText && (
        element.innerText.includes('已连接') || element.innerText.includes('访问密钥')
      )) || items.find((element) => element.innerText && (
        element.innerText.includes('已连接') || element.innerText.includes('访问密钥')
      ));
    if (!item) return false;
    if (scroll) scroll.scrollTop = Math.max(0, item.offsetTop - 120);
    if (!visible(item)) return false;
    item.click();
    return true;
  })()`);
  assert(clicked, 'access key item not found');
  await sleep(400);
}

async function clickLaneStat(marker, label) {
  const clicked = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    };
    const lane = Array.from(document.querySelectorAll('.branch-lane'))
      .find((element) => element.innerText.includes(${JSON.stringify(marker)}));
    lane?.scrollIntoView({ block: 'center', inline: 'nearest' });
    const link = Array.from(lane?.querySelectorAll('a') || [])
      .find((element) => element.innerText.includes(${JSON.stringify(label)}));
    if (!link) return false;
    link.scrollIntoView({ block: 'center', inline: 'nearest' });
    if (!visible(link)) return false;
    link.click();
    return true;
  })()`);
  assert(clicked, `lane stat not found: ${marker} / ${label}`);
  await sleep(500);
}

async function clickLaneOpenButton(marker) {
  const clicked = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    };
    const card = Array.from(document.querySelectorAll('.lane-session-card'))
      .find((element) => element.innerText.includes(${JSON.stringify(marker)}));
    card?.scrollIntoView({ block: 'center', inline: 'nearest' });
    const button = card?.querySelector('[aria-label="打开卡片"]');
    if (!button || !visible(button)) return false;
    button.click();
    return true;
  })()`);
  assert(clicked, `lane open button not found: ${marker}`);
  await sleep(500);
}

async function toggleLaneForMarker(marker) {
  const clicked = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    };
    const lane = Array.from(document.querySelectorAll('.branch-lane'))
      .find((element) => visible(element) && element.innerText.includes(${JSON.stringify(marker)}));
    const header = lane?.querySelector('.q-item');
    if (!header || !visible(header)) return false;
    header.click();
    return true;
  })()`);
  assert(clicked, `lane toggle button not found: ${marker}`);
  await sleep(500);
}

async function toggleLaneAtIndex(index) {
  const clicked = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    };
    const lane = Array.from(document.querySelectorAll('.branch-lane')).filter(visible)[${index}];
    const header = lane?.querySelector('.q-item');
    if (!header || !visible(header)) return false;
    header.click();
    return true;
  })()`);
  assert(clicked, `lane toggle button not found at index ${index}`);
  await sleep(500);
}

async function ensureLaneAtIndexExpanded(index) {
  if (!(await laneAtIndexExpanded(index))) {
    await toggleLaneAtIndex(index);
    await waitForLaneAtIndexExpanded(index, true);
  }
}

async function waitForLaneAtIndexExpanded(index, expected, timeoutMs = 10_000) {
  const started = Date.now();
  while (Date.now() - started < timeoutMs) {
    if ((await laneAtIndexExpanded(index)) === expected) return;
    await sleep(250);
  }
  throw new Error(`Timed out waiting for lane ${index} expanded=${expected}`);
}

async function laneAtIndexExpanded(index) {
  return evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    };
    const lane = Array.from(document.querySelectorAll('.branch-lane')).filter(visible)[${index}];
    return Boolean(lane?.classList.contains('q-expansion-item--expanded'));
  })()`);
}

async function ensureLaneExpanded(marker) {
  const expanded = await laneExpanded(marker);
  if (!expanded) {
    await toggleLaneHeaderByBranch('', 'main');
    await waitForLaneExpanded(marker, true);
  }
}

async function waitForLaneExpanded(marker, expected, timeoutMs = 10_000) {
  const started = Date.now();
  while (Date.now() - started < timeoutMs) {
    if ((await laneExpanded(marker)) === expected) return;
    await sleep(250);
  }
  throw new Error(`Timed out waiting for lane expanded=${expected}`);
}

async function laneExpanded(marker) {
  return evaluate(`Array.from(document.querySelectorAll('.branch-lane')).some((lane) => {
    const rect = lane.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0 &&
      lane.classList.contains('q-expansion-item--expanded') &&
      lane.innerText.includes(${JSON.stringify(marker)});
  })`);
}

async function toggleLaneHeaderByBranch(projectName, branch) {
  const clicked = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    };
    const lane = Array.from(document.querySelectorAll('.branch-lane'))
      .find((element) => visible(element) &&
        element.innerText.includes(${JSON.stringify(branch)}) &&
        (!${JSON.stringify(projectName)} || element.innerText.includes(${JSON.stringify(projectName)})));
    const header = lane?.querySelector('.q-item');
    if (!header || !visible(header)) return false;
    header.click();
    return true;
  })()`);
  assert(clicked, `lane toggle button not found: ${projectName} / ${branch}`);
  await sleep(500);
}

async function clickCard(marker) {
  const clicked = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    };
    const card = Array.from(document.querySelectorAll('.lane-session-card'))
      .find((element) => element.innerText.includes(${JSON.stringify(marker)}));
    if (!card) return false;
    card.scrollIntoView({ block: 'center', inline: 'nearest' });
    if (!visible(card)) return false;
    card.click();
    return true;
  })()`);
  assert(clicked, `card not found: ${marker}`);
  await sleep(500);
}

async function setInput(selector, value) {
  const changed = await evaluate(`(() => {
    const input = document.querySelector(${JSON.stringify(selector)});
    if (!input) return false;
    input.focus();
    const setter = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(input), 'value')?.set;
    setter.call(input, ${JSON.stringify(value)});
    input.dispatchEvent(new Event('input', { bubbles: true }));
    input.dispatchEvent(new Event('change', { bubbles: true }));
    return true;
  })()`);
  assert(changed, `input not found: ${selector}`);
  await sleep(600);
}

async function attachNewSessionFixtureFiles() {
  const fixtureDir = `/tmp/anycode-click-audit-attachments-${stamp}`;
  await mkdir(fixtureDir, { recursive: true });
  const files = [
    `${fixtureDir}/click-audit.md`,
    `${fixtureDir}/click-audit.png`,
  ];
  await writeFile(files[0], '# Click audit attachment\n');
  await writeFile(files[1], 'AnyCode click audit image placeholder\n');
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

async function attachDetailComposerFixtureFile() {
  const fixtureDir = `/tmp/anycode-click-audit-detail-${stamp}`;
  await mkdir(fixtureDir, { recursive: true });
  const file = `${fixtureDir}/detail-audit.png`;
  await writeFile(file, 'AnyCode detail composer image placeholder\n');
  const documentNode = await page.send('DOM.getDocument', { depth: -1, pierce: true });
  const inputNode = await page.send('DOM.querySelector', {
    nodeId: documentNode.root.nodeId,
    selector: '.detail-composer input[type="file"]',
  });
  assert(inputNode.nodeId, 'detail composer file input not found');
  await page.send('DOM.setFileInputFiles', {
    nodeId: inputNode.nodeId,
    files: [file],
  });
  await sleep(500);
}

async function removeAttachmentChip(filename) {
  const clicked = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0;
    };
    const chip = Array.from(document.querySelectorAll('.attachment-chip'))
      .find((element) => visible(element) && element.innerText.includes(${JSON.stringify(filename)}));
    const remove = chip?.querySelector('.q-chip__icon--remove');
    if (!remove) return false;
    remove.click();
    return true;
  })()`);
  assert(clicked, `attachment remove button not found: ${filename}`);
  await sleep(400);
}

async function closeVisibleDialog() {
  const closed = await evaluate(`(() => {
    const visible = (element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0;
    };
    const dialog = Array.from(document.querySelectorAll('.q-dialog')).find(visible);
    const target = dialog?.querySelector('[aria-label="关闭"], [aria-label="取消"]');
    if (!target || !visible(target)) return false;
    target.click();
    return true;
  })()`);
  assert(closed, 'visible dialog close button not found');
  await sleep(300);
}

async function closeAllDialogs() {
  for (let index = 0; index < 5; index += 1) {
    const closed = await evaluate(`(() => {
      const visible = (element) => {
        const rect = element.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
      };
      const dialog = Array.from(document.querySelectorAll('.q-dialog')).find(visible);
      const target = dialog?.querySelector('[aria-label="关闭"], [aria-label="取消"], .q-dialog__backdrop');
      if (!target) return false;
      target.click();
      return true;
    })()`);
    if (!closed) return;
    await sleep(150);
  }
}

async function waitForNoDialog(timeoutMs = 5_000) {
  const started = Date.now();
  while (Date.now() - started < timeoutMs) {
    const hasDialog = await evaluate(`Array.from(document.querySelectorAll('.q-dialog')).some((element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    })`);
    if (!hasDialog) return;
    await sleep(100);
  }
  throw new Error('dialog is still visible');
}

async function elementCount(selector) {
  return evaluate(`document.querySelectorAll(${JSON.stringify(selector)}).length`);
}

async function screenshot(name) {
  const shot = await page.send('Page.captureScreenshot', {
    format: 'png',
    captureBeyondViewport: false,
  });
  await writeFile(`${screenshotDir}/${name}`, Buffer.from(shot.data, 'base64'));
}

async function graphql(query, variables = {}) {
  const response = await fetch(`${baseURL}/graphql`, {
    method: 'POST',
    headers: {
      'content-type': 'application/json',
      authorization: `Bearer ${accessKey}`,
    },
    body: JSON.stringify({ query, variables }),
  });
  const body = await response.json();
  if (!response.ok || body.errors?.length) {
    throw new Error(JSON.stringify(body.errors || body));
  }
  return body.data;
}

function assert(value, message) {
  if (!value) throw new Error(message);
}

function errorMessage(error) {
  return error instanceof Error ? error.message : String(error);
}
