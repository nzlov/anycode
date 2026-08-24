import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const dialogSource = readFileSync(
  new URL('../src/components/ProjectDirectoryDialog.vue', import.meta.url),
  'utf8',
);
const projectsPageSource = readFileSync(
  new URL('../src/pages/ProjectsPage.vue', import.meta.url),
  'utf8',
);
const projectsServiceSource = readFileSync(
  new URL('../src/services/projects.ts', import.meta.url),
  'utf8',
);
const quasarConfigSource = readFileSync(new URL('../quasar.config.ts', import.meta.url), 'utf8');

test('projects page opens the add-project dialog on desktop and keeps the mobile route', () => {
  assert.match(projectsPageSource, /<ProjectDirectoryDialog v-model="createDialogOpen"/);
  assert.match(projectsPageSource, /aria-label="新增项目"[\s\S]*?@click="openCreateProject"/);
  assert.match(
    projectsPageSource,
    /function openCreateProject[\s\S]*?\$q\.screen\.lt\.sm[\s\S]*?name: 'project-create'[\s\S]*?createDialogOpen\.value = true/,
  );
  assert.doesNotMatch(projectsPageSource, /:to="\{ name: 'project-create' \}"/);
});

test('add-project dialog supports local and remote sources', () => {
  assert.match(dialogSource, /label: '本地项目', value: 'local'/);
  assert.match(dialogSource, /label: '远程项目', value: 'remote'/);
  assert.match(dialogSource, /v-model="repositoryURL"[\s\S]*?label="项目地址"/);
  assert.match(dialogSource, /本地克隆父目录/);
  assert.match(
    dialogSource,
    /await createRemoteProject\(selected\.value, repositoryURL\.value\.trim\(\)\)/,
  );
});

test('remote creation keeps the dialog open while a global loading overlay blocks interaction', () => {
  assert.match(quasarConfigSource, /plugins: \['Dialog', 'Loading', 'Notify'\]/);
  assert.match(dialogSource, /:persistent="persistent \|\| creating"/);
  assert.match(dialogSource, /\$q\.loading\.show\(\{ message: '正在克隆并添加远程项目…' \}\)/);
  assert.match(
    dialogSource,
    /await createRemoteProject[\s\S]*?emit\('update:modelValue', false\)[\s\S]*?finally[\s\S]*?\$q\.loading\.hide\(\)/,
  );
});

test('project service uses the dedicated clone-and-create mutation', () => {
  assert.match(projectsServiceSource, /mutation CloneProject\(\$input: CloneProjectInput!\)/);
  assert.match(projectsServiceSource, /cloneProject\(input: \$input\)/);
});
