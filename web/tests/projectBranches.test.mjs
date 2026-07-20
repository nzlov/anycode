import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';

test('project list no longer requests git state for sidebar summaries', () => {
  const source = readFileSync(new URL('../src/services/projects.ts', import.meta.url), 'utf8');
  const listProjectsBody = source.match(/export async function listProjects\(\) \{(?<body>[\s\S]*?)\n\}/)?.groups?.body ?? '';

  assert.doesNotMatch(listProjectsBody, /gitState/);
});

test('new session dialog refreshes cached branches explicitly', () => {
  const source = readFileSync(new URL('../src/components/NewSessionDialog.vue', import.meta.url), 'utf8');

  assert.equal((source.match(/hide-dropdown-icon/g) ?? []).length, 3);
  assert.doesNotMatch(source, /dropdown-icon=""/);
  assert.match(
    source,
    /label="基础分支"[\s\S]*hide-dropdown-icon[\s\S]*<template #append>[\s\S]*aria-label="刷新分支"[\s\S]*@click\.stop="refreshProjectBranches\(projectId\)"[\s\S]*<\/template>[\s\S]*<\/q-select>/,
  );
  assert.match(
    source,
    /<template #selected>\s*<span class="ellipsis">[\s\S]*基础分支：\{\{ branch \}\}[\s\S]*<\/span>\s*<\/template>/,
  );
  assert.doesNotMatch(source, /<div v-if="selectedProject\?\.isGit" class="branch-picker">/);
  assert.match(source, /refreshProjectBranches[\s\S]*loadBranchesForProject\(value,\s*\{\s*refresh:\s*true\s*\}/);
  assert.match(source, /loadBranchesForProject[\s\S]*loadProjectBranches\(value,\s*options\)/);
  assert.match(
    source,
    /branch\.value\s*=\s*state\.branches\.includes\(branch\.value\)\s*\?\s*branch\.value\s*:\s*state\.defaultBranch/,
  );
  assert.match(source, /message:\s*`获取分支失败：\$\{errorMessage\(error\)\}`/);
});

test('switching projects waits for refreshed branches before creating a session', () => {
  const source = readFileSync(
    new URL('../src/components/NewSessionDialog.vue', import.meta.url),
    'utf8',
  );

  assert.match(
    source,
    /watch\(projectId,[\s\S]*branch\.value\s*=\s*'';[\s\S]*loadBranchesForProject\(value,\s*\{\s*refresh:\s*true\s*\}\)/,
  );
  assert.match(source, /:disable="creating \|\| !branchSelectionReady \|\| !codexConfigReady"/);
  assert.match(source, /:inert="branchesLoading"/);
  assert.match(source, /:aria-busy="branchesLoading"/);
  assert.match(source, /<q-inner-loading\s+:showing="branchesLoading"/);
  assert.match(
    source,
    /async function createSession\(requestedMode: 'workflow' \| 'chat'\)[\s\S]*if \(!branchSelectionReady\.value\)/,
  );
  const createSessionSource = source.match(
    /async function createSession\(requestedMode: 'workflow' \| 'chat'\) \{(?<body>[\s\S]*?)\n\}/,
  )?.groups?.body;
  assert.ok(createSessionSource);
  assert.ok(createSessionSource.indexOf('const input: CreateSessionInput') < createSessionSource.indexOf('await stageAttachment'));
  assert.match(createSessionSource, /rememberProjectId\(input\.projectId\)/);
});

test('concurrent branch loads reuse the active request', async () => {
  let calls = 0;
  let resolveRequest;
  const request = new Promise((resolve) => {
    resolveRequest = resolve;
  });
  const { useProjectBranches } = loadProjectBranchesModule(() => {
    calls += 1;
    return request;
  });
  const { loadProjectBranches } = useProjectBranches();

  const first = loadProjectBranches('project-1', { refresh: true });
  const second = loadProjectBranches('project-1', { refresh: true });
  assert.equal(calls, 1);

  resolveRequest({ defaultBranch: 'master', branches: ['master'] });
  assert.deepEqual(await first, { defaultBranch: 'master', branches: ['master'] });
  assert.deepEqual(await second, { defaultBranch: 'master', branches: ['master'] });
});

test('failed refresh invalidates stale project branches', async () => {
  let calls = 0;
  const { useProjectBranches } = loadProjectBranchesModule(() => {
    calls += 1;
    if (calls === 1) return Promise.resolve({ defaultBranch: 'main', branches: ['main'] });
    return Promise.reject(new Error('refresh failed'));
  });
  const { branchCache, branchLoading, loadProjectBranches } = useProjectBranches();

  await loadProjectBranches('project-1');
  assert.deepEqual(branchCache.value['project-1'], {
    defaultBranch: 'main',
    branches: ['main'],
  });

  const refresh = loadProjectBranches('project-1', { refresh: true });
  assert.equal(branchCache.value['project-1'], undefined);
  assert.equal(branchLoading.value['project-1'], true);
  await assert.rejects(refresh, /refresh failed/);
  assert.equal(branchCache.value['project-1'], undefined);
  assert.equal(branchLoading.value['project-1'], undefined);
});

test('project branch cache is isolated from project list state', () => {
  const projectsSource = readFileSync(new URL('../src/composables/useProjects.ts', import.meta.url), 'utf8');
  const branchesSource = readFileSync(new URL('../src/composables/useProjectBranches.ts', import.meta.url), 'utf8');
  const dialogSource = readFileSync(new URL('../src/components/NewSessionDialog.vue', import.meta.url), 'utf8');

  assert.doesNotMatch(projectsSource, /branchCache|branchLoading|getProjectBranches|loadProjectBranches/);
  assert.match(branchesSource, /branchCache/);
  assert.match(branchesSource, /loadProjectBranches/);
  assert.match(dialogSource, /useProjectBranches/);
});

function loadProjectBranchesModule(getProjectBranches) {
  const source = readFileSync(
    new URL('../src/composables/useProjectBranches.ts', import.meta.url),
    'utf8',
  );
  const compiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
    },
  }).outputText;
  const module = { exports: {} };
  const testRequire = (id) => {
    if (id === 'vue') return { ref: (value) => ({ value }) };
    if (id === '@/services/projects') return { getProjectBranches };
    throw new Error(`Unexpected test import: ${id}`);
  };
  new Function('require', 'module', 'exports', compiled)(testRequire, module, module.exports);
  return module.exports;
}
