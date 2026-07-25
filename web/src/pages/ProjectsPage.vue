<template>
  <q-page class="surface-page project-list-page">
    <PageToolbar title="项目" title-icon="folder" />

    <q-card flat bordered class="surface-page__card project-list-card">
      <q-linear-progress v-if="loading" indeterminate color="primary" />
      <q-card-section class="project-list-card__body">
        <q-banner v-if="loadError" dense rounded class="quick-command-error">
          <template #avatar>
            <q-icon name="error_outline" color="negative" />
          </template>
          {{ loadError }}
          <template #action>
            <q-btn flat dense label="重试" no-caps @click="refreshProjects" />
          </template>
        </q-banner>

        <q-list v-if="projects.length" bordered separator class="global-project-list">
          <q-item
            v-for="project in projects"
            :key="project.id"
            clickable
            :disable="loading || removing"
            @click="openProjectOverview(project.id)"
          >
            <q-item-section avatar>
              <q-icon :name="project.isGit ? 'folder_open' : 'folder'" color="primary" />
            </q-item-section>
            <q-item-section class="global-project-list__content">
              <q-item-label>{{ project.name }}</q-item-label>
              <q-item-label caption lines="1" class="global-project-list__path">
                {{ project.path }}
              </q-item-label>
            </q-item-section>
            <q-item-section v-if="project.isGit" side class="global-project-list__git">
              <q-badge outline color="positive" label="Git" />
            </q-item-section>
            <q-item-section side>
              <q-btn
                flat
                round
                dense
                class="app-icon-btn"
                icon="more_vert"
                :aria-label="`${project.name} 项目操作`"
                @click.stop
              >
                <q-menu>
                  <q-list dense class="project-menu app-touch-list">
                    <q-item v-close-popup clickable @click.stop="openProjectSettings(project)">
                      <q-item-section avatar>
                        <q-icon name="settings" />
                      </q-item-section>
                      <q-item-section>设置</q-item-section>
                    </q-item>
                    <q-item v-close-popup clickable @click.stop="openWorkflowConfig(project.id)">
                      <q-item-section avatar>
                        <q-icon name="account_tree" />
                      </q-item-section>
                      <q-item-section>流程配置</q-item-section>
                    </q-item>
                    <q-item
                      v-close-popup
                      clickable
                      class="text-negative"
                      @click.stop="confirmRemoveProject(project.id, project.name)"
                    >
                      <q-item-section avatar>
                        <q-icon name="playlist_remove" />
                      </q-item-section>
                      <q-item-section>移除项目</q-item-section>
                    </q-item>
                  </q-list>
                </q-menu>
              </q-btn>
            </q-item-section>
          </q-item>
        </q-list>
        <div v-else-if="!loading && !loadError" class="global-settings-empty">暂无项目</div>
      </q-card-section>
    </q-card>

    <q-page-sticky position="bottom-right" :offset="[24, 24]">
      <q-btn
        fab
        color="primary"
        class="app-on-primary"
        icon="add"
        aria-label="新增项目"
        :disable="loading"
        :to="{ name: 'project-create' }"
      >
        <q-tooltip>新增项目</q-tooltip>
      </q-btn>
    </q-page-sticky>

    <q-dialog v-model="removeDialogOpen">
      <q-card class="confirm-dialog">
        <q-card-section class="row items-center q-pb-sm">
          <div class="text-subtitle1 text-weight-bold">移除项目</div>
          <q-space />
          <q-btn v-close-popup flat round dense class="app-icon-btn" icon="close" aria-label="关闭">
            <q-tooltip>关闭</q-tooltip>
          </q-btn>
        </q-card-section>
        <q-separator />
        <q-card-section>
          <div class="text-body2">确认移除项目“{{ removingProjectName }}”？</div>
        </q-card-section>
        <q-card-actions align="right">
          <q-btn
            v-close-popup
            flat
            round
            class="app-icon-btn"
            icon="close"
            color="primary"
            aria-label="取消"
          >
            <q-tooltip>取消</q-tooltip>
          </q-btn>
          <q-btn
            unelevated
            class="app-command-btn"
            color="negative"
            icon="playlist_remove"
            label="移除"
            no-caps
            :loading="removing"
            @click="removeSelectedProject"
          />
        </q-card-actions>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';

import PageToolbar from '@/components/PageToolbar.vue';
import { useProjects } from '@/composables/useProjects';
import type { ProjectSummary } from '@/services/projects';

const router = useRouter();
const { projects, loading, loadProjects, removeProjectById } = useProjects();
const loadError = ref('');
const removeDialogOpen = ref(false);
const removingProjectId = ref('');
const removingProjectName = ref('');
const removing = ref(false);

onMounted(refreshProjects);

async function refreshProjects() {
  loadError.value = '';
  try {
    await loadProjects();
  } catch {
    loadError.value = '无法加载项目';
  }
}

function openProjectOverview(projectId: string) {
  void router.push({ name: 'overview', query: { projectId } });
}

function openProjectSettings(project: ProjectSummary) {
  void router.push({ name: 'project-settings', params: { projectId: project.id } });
}

function openWorkflowConfig(projectId: string) {
  void router.push({ name: 'workflow-config', params: { projectId } });
}

function confirmRemoveProject(projectId: string, projectName: string) {
  removingProjectId.value = projectId;
  removingProjectName.value = projectName;
  removeDialogOpen.value = true;
}

async function removeSelectedProject() {
  if (!removingProjectId.value) return;
  removing.value = true;
  try {
    await removeProjectById(removingProjectId.value);
    removeDialogOpen.value = false;
  } finally {
    removing.value = false;
  }
}
</script>

<style scoped>
.project-list-card__body {
  display: flex;
  min-height: calc(100dvh - 100px);
  flex-direction: column;
}
</style>
