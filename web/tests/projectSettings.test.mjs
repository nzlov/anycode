import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

test('project settings API preserves the worktree init command', () => {
  const source = readFileSync(new URL('../src/services/projects.ts', import.meta.url), 'utf8');

  assert.match(source, /worktreeInitCommand/);
  assert.match(source, /mutation UpdateProjectSettings/);
  assert.match(source, /updateProjectSettings\(input: \$input\)/);
  assert.doesNotMatch(source, /worktreeInitCommand:\s*input\.worktreeInitCommand\.trim/);
});

test('project menu opens settings before workflow configuration and removal', () => {
  const source = readFileSync(new URL('../src/layouts/MainLayout.vue', import.meta.url), 'utf8');
  const settings = source.indexOf('<q-item-section>设置</q-item-section>');
  const workflow = source.indexOf('<q-item-section>流程配置</q-item-section>');
  const remove = source.indexOf('<q-item-section>移除项目</q-item-section>');

  assert.ok(settings >= 0 && settings < workflow && workflow < remove);
  assert.match(source, /openProjectSettings\(project\)/);
  assert.match(source, /<project-settings-dialog/);
});

test('project settings dialog uses a multiline input and submits the raw value', () => {
  const source = readFileSync(
    new URL('../src/components/ProjectSettingsDialog.vue', import.meta.url),
    'utf8',
  );

  assert.match(source, /type="textarea"/);
  assert.match(source, /label="工作树初始化命令"/);
  assert.match(source, /worktreeInitCommand:\s*worktreeInitCommand\.value/);
  assert.doesNotMatch(source, /worktreeInitCommand\.value\.trim/);
  assert.match(source, /:persistent="saving"/);
  assert.match(source, /aria-label="项目设置"/);
  assert.equal(source.match(/:disable="saving"/g)?.length, 2);
});

test('updating project settings preserves client-only project state', () => {
  const source = readFileSync(
    new URL('../src/composables/useProjects.ts', import.meta.url),
    'utf8',
  );

  assert.match(source, /active:\s*current\.active/);
  assert.match(source, /openSessions:\s*current\.openSessions/);
});
