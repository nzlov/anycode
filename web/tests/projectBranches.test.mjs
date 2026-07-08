import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

test('project list no longer requests git state for sidebar summaries', () => {
  const source = readFileSync(new URL('../src/services/projects.ts', import.meta.url), 'utf8');
  const listProjectsBody = source.match(/export async function listProjects\(\) \{(?<body>[\s\S]*?)\n\}/)?.groups?.body ?? '';

  assert.doesNotMatch(listProjectsBody, /gitState/);
});

test('new session dialog refreshes cached branches explicitly', () => {
  const source = readFileSync(new URL('../src/components/NewSessionDialog.vue', import.meta.url), 'utf8');

  assert.match(source, /aria-label="刷新分支"/);
  assert.match(source, /refreshProjectBranches\(projectId\)/);
  assert.match(source, /refreshProjectBranches[\s\S]*loadBranchesForProject\(value,\s*\{\s*refresh:\s*true\s*\}/);
  assert.match(source, /loadBranchesForProject[\s\S]*loadProjectBranches\(value,\s*options\)/);
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
