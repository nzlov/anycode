<template>
  <q-dialog
    :model-value="modelValue"
    :maximized="$q.screen.lt.sm"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <q-card class="global-settings-dialog app-content-dialog">
      <q-card-section class="global-settings-header row items-center">
        <div class="text-subtitle1 text-weight-bold">全局设置</div>
        <q-space />
        <q-btn v-close-popup flat round dense class="app-icon-btn" icon="close" aria-label="关闭">
          <q-tooltip>关闭</q-tooltip>
        </q-btn>
      </q-card-section>

      <q-separator />

      <q-tabs v-model="activeSection" dense align="left" no-caps class="global-settings-tabs lt-sm">
        <q-tab name="projects" icon="folder" label="项目" />
        <q-tab name="appearance" icon="palette" label="外观" />
        <q-tab name="notifications" icon="notifications" label="通知" />
        <q-tab name="quick_commands" icon="bolt" label="快捷指令" />
      </q-tabs>

      <div class="global-settings-grid">
        <nav class="global-settings-nav gt-xs" aria-label="全局设置分类">
          <q-list padding>
            <q-item
              clickable
              :active="activeSection === 'projects'"
              active-class="global-settings-nav__active"
              @click="activeSection = 'projects'"
            >
              <q-item-section avatar>
                <q-icon name="folder" />
              </q-item-section>
              <q-item-section>项目</q-item-section>
            </q-item>
            <q-item
              clickable
              :active="activeSection === 'appearance'"
              active-class="global-settings-nav__active"
              @click="activeSection = 'appearance'"
            >
              <q-item-section avatar>
                <q-icon name="palette" />
              </q-item-section>
              <q-item-section>外观</q-item-section>
            </q-item>
            <q-item
              clickable
              :active="activeSection === 'notifications'"
              active-class="global-settings-nav__active"
              @click="activeSection = 'notifications'"
            >
              <q-item-section avatar>
                <q-icon name="notifications" />
              </q-item-section>
              <q-item-section>通知</q-item-section>
            </q-item>
            <q-item
              clickable
              :active="activeSection === 'quick_commands'"
              active-class="global-settings-nav__active"
              @click="activeSection = 'quick_commands'"
            >
              <q-item-section avatar>
                <q-icon name="bolt" />
              </q-item-section>
              <q-item-section>快捷指令</q-item-section>
            </q-item>
          </q-list>
        </nav>

        <section v-if="activeSection === 'projects'" class="global-settings-panel">
          <div class="global-settings-panel__header">
            <div class="text-subtitle2 text-weight-bold">项目</div>
          </div>

          <q-linear-progress v-if="projectsLoading" indeterminate color="primary" />
          <q-list v-if="projects.length" bordered separator class="global-project-list">
            <q-item
              v-for="project in projects"
              :key="project.id"
              clickable
              :disable="projectsLoading || removingProject"
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
          <div v-else class="global-settings-empty">
            <q-spinner v-if="projectsLoading" color="primary" size="24px" />
            <template v-else>暂无项目</template>
          </div>

          <q-btn
            fab
            color="primary"
            class="global-settings-add-fab app-on-primary"
            icon="add"
            aria-label="新增项目"
            :disable="projectsLoading"
            @click="directoryDialogOpen = true"
          >
            <q-tooltip>新增项目</q-tooltip>
          </q-btn>
        </section>

        <section v-else-if="activeSection === 'appearance'" class="global-settings-panel">
          <div class="global-settings-panel__header">
            <div class="text-subtitle2 text-weight-bold">外观</div>
          </div>

          <q-banner v-if="appearanceError" dense class="quick-command-error">
            <template #avatar>
              <q-icon name="error_outline" color="negative" />
            </template>
            {{ appearanceError }}
            <template #action>
              <q-btn
                flat
                round
                dense
                class="app-icon-btn"
                icon="refresh"
                aria-label="重试加载外观设置"
                @click="refreshAppearance"
              >
                <q-tooltip>重试</q-tooltip>
              </q-btn>
            </template>
          </q-banner>

          <q-linear-progress v-if="appearanceLoading" indeterminate color="primary" />
          <q-list bordered separator class="appearance-settings-list">
            <q-item>
              <q-item-section avatar>
                <q-icon name="format_color_fill" color="primary" />
              </q-item-section>
              <q-item-section>
                <q-item-label>壁纸选色算法</q-item-label>
              </q-item-section>
              <q-item-section side class="appearance-settings-list__control">
                <q-select
                  v-model="wallpaperColorScheme"
                  outlined
                  dense
                  emit-value
                  map-options
                  options-dense
                  :options="wallpaperColorSchemeOptions"
                  :disable="appearanceLoading || appearanceSaving"
                  aria-label="壁纸选色算法"
                  @update:model-value="saveWallpaperColorScheme"
                />
              </q-item-section>
            </q-item>
          </q-list>
        </section>

        <section v-else-if="activeSection === 'notifications'" class="global-settings-panel">
          <div class="global-settings-panel__header">
            <div class="text-subtitle2 text-weight-bold">通知</div>
          </div>

          <q-banner v-if="notificationError" dense class="quick-command-error">
            <template #avatar>
              <q-icon name="error_outline" color="negative" />
            </template>
            {{ notificationError }}
            <template #action>
              <q-btn
                flat
                round
                dense
                class="app-icon-btn"
                icon="refresh"
                aria-label="重试加载通知设置"
                @click="refreshNotifications"
              >
                <q-tooltip>重试</q-tooltip>
              </q-btn>
            </template>
          </q-banner>

          <q-linear-progress v-if="notificationLoading" indeterminate color="primary" />
          <q-list bordered separator class="appearance-settings-list">
            <q-item>
              <q-item-section avatar>
                <q-icon name="notifications_active" color="primary" />
              </q-item-section>
              <q-item-section>
                <q-item-label>卡片系统通知</q-item-label>
                <q-item-label caption>{{ notificationCaption }}</q-item-label>
              </q-item-section>
              <q-item-section side>
                <q-toggle
                  :model-value="notificationState.enabled"
                  :disable="
                    notificationLoading || notificationSaving || !notificationToggleAvailable
                  "
                  color="primary"
                  aria-label="卡片系统通知"
                  @update:model-value="setNotificationsEnabled"
                />
              </q-item-section>
            </q-item>
          </q-list>
        </section>

        <section v-else class="global-settings-panel">
          <div class="global-settings-panel__header">
            <div class="text-subtitle2 text-weight-bold">快捷指令</div>
          </div>

          <q-banner v-if="quickCommandsError" dense class="quick-command-error">
            <template #avatar>
              <q-icon name="error_outline" color="negative" />
            </template>
            {{ quickCommandsError }}
            <template #action>
              <q-btn
                flat
                round
                dense
                class="app-icon-btn"
                icon="refresh"
                aria-label="重试加载快捷指令"
                @click="refreshQuickCommands"
              >
                <q-tooltip>重试</q-tooltip>
              </q-btn>
            </template>
          </q-banner>

          <q-slide-transition>
            <div v-if="adding" class="quick-command-editor">
              <q-input
                ref="commandInputRef"
                v-model="draftCommand"
                outlined
                autogrow
                label="快捷指令"
                :disable="saving"
                @keyup.ctrl.enter="saveCommand"
              />
              <div class="quick-command-editor__actions">
                <q-btn
                  flat
                  round
                  class="app-icon-btn"
                  icon="close"
                  aria-label="取消新增"
                  :disable="saving"
                  @click="cancelAdd"
                >
                  <q-tooltip>取消</q-tooltip>
                </q-btn>
                <q-btn
                  unelevated
                  round
                  color="primary"
                  class="app-icon-btn app-on-primary"
                  icon="check"
                  aria-label="保存快捷指令"
                  :loading="saving"
                  :disable="saving || !draftCommand.trim()"
                  @click="saveCommand"
                >
                  <q-tooltip>保存</q-tooltip>
                </q-btn>
              </div>
            </div>
          </q-slide-transition>

          <q-linear-progress
            v-if="quickCommandsLoading && quickCommands.length"
            indeterminate
            color="primary"
          />
          <q-list v-if="quickCommands.length" separator class="quick-command-list">
            <q-item
              v-for="command in quickCommands"
              :key="command.id"
              :disable="quickCommandsLoading"
            >
              <q-item-section>
                <q-item-label class="quick-command-text">{{ command.content }}</q-item-label>
              </q-item-section>
              <q-item-section side>
                <q-btn
                  flat
                  round
                  dense
                  class="app-icon-btn"
                  color="negative"
                  icon="delete_outline"
                  :aria-label="`删除快捷指令：${command.content}`"
                  :loading="deletingCommandIds.includes(command.id)"
                  :disable="quickCommandsLoading || quickCommandsMutating > 0"
                  @click="removeCommand(command.id)"
                >
                  <q-tooltip>删除</q-tooltip>
                </q-btn>
              </q-item-section>
            </q-item>
          </q-list>
          <div v-else-if="!quickCommandsError" class="global-settings-empty">
            <q-spinner v-if="quickCommandsLoading" color="primary" size="24px" />
            <template v-else>暂无快捷指令</template>
          </div>

          <AppPagination
            v-if="quickCommandPageMax > 1"
            :model-value="quickCommandsPageInfo.page"
            :max="quickCommandPageMax"
            :disabled="quickCommandsLoading || quickCommandsMutating > 0"
            class="quick-command-pagination"
            @update:model-value="changeQuickCommandPage"
          />

          <q-btn
            fab
            color="primary"
            class="global-settings-add-fab app-on-primary"
            icon="add"
            aria-label="新增快捷指令"
            :disable="adding || quickCommandsLoading || quickCommandsMutating > 0"
            @click="startAdd"
          >
            <q-tooltip>新增快捷指令</q-tooltip>
          </q-btn>
        </section>
      </div>

      <project-directory-dialog v-model="directoryDialogOpen" />
      <project-settings-dialog v-model="projectSettingsOpen" :project="settingsProject" />

      <q-dialog v-model="removeProjectDialogOpen">
        <q-card class="confirm-dialog">
          <q-card-section class="row items-center q-pb-sm">
            <div class="text-subtitle1 text-weight-bold">移除项目</div>
            <q-space />
            <q-btn
              v-close-popup
              flat
              round
              dense
              class="app-icon-btn"
              icon="close"
              aria-label="关闭"
            >
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
              :loading="removingProject"
              @click="removeSelectedProject"
            />
          </q-card-actions>
        </q-card>
      </q-dialog>
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';

import AppPagination from '@/components/AppPagination.vue';
import ProjectDirectoryDialog from '@/components/ProjectDirectoryDialog.vue';
import ProjectSettingsDialog from '@/components/ProjectSettingsDialog.vue';
import { useProjects } from '@/composables/useProjects';
import { useQuickCommands } from '@/composables/useQuickCommands';
import {
  getAppearanceSettings,
  updateAppearanceSettings,
  wallpaperColorSchemeOptions,
} from '@/services/appearanceSettings';
import type { ProjectSummary } from '@/services/projects';
import {
  disablePushNotifications,
  enablePushNotifications,
  getPushNotificationState,
  type PushNotificationState,
} from '@/services/pushNotifications';
import { setWallpaperColorScheme } from '@/theme/dailyBackground';
import type { WallpaperColorScheme } from '@/theme/dailyBackgroundModel';

const props = defineProps<{
  modelValue: boolean;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
}>();

const route = useRoute();
const router = useRouter();
const activeSection = ref<'projects' | 'appearance' | 'notifications' | 'quick_commands'>(
  'projects',
);
const directoryDialogOpen = ref(false);
const projectSettingsOpen = ref(false);
const settingsProject = ref<ProjectSummary | null>(null);
const removeProjectDialogOpen = ref(false);
const removingProjectId = ref('');
const removingProjectName = ref('');
const removingProject = ref(false);
const { projects, loading: projectsLoading, loadProjects, removeProjectById } = useProjects();
const {
  quickCommands,
  quickCommandsLoading,
  quickCommandsMutating,
  quickCommandsError,
  quickCommandsPageInfo,
  loadQuickCommands,
  addQuickCommand,
  deleteQuickCommand,
} = useQuickCommands();
const adding = ref(false);
const draftCommand = ref('');
const saving = ref(false);
const deletingCommandIds = ref<string[]>([]);
const commandInputRef = ref<{ focus: () => void } | null>(null);
const quickCommandPageMax = computed(() =>
  Math.max(1, Math.ceil(quickCommandsPageInfo.value.total / quickCommandsPageInfo.value.pageSize)),
);
const appearanceLoading = ref(false);
const appearanceSaving = ref(false);
const appearanceError = ref('');
const wallpaperColorScheme = ref<WallpaperColorScheme>('content');
const persistedWallpaperColorScheme = ref<WallpaperColorScheme>('content');
const notificationLoading = ref(false);
const notificationSaving = ref(false);
const notificationError = ref('');
const notificationState = ref<PushNotificationState>({
  supported: true,
  available: true,
  permission: 'default',
  enabled: false,
});
const notificationToggleAvailable = computed(
  () =>
    notificationState.value.supported &&
    notificationState.value.available &&
    notificationState.value.permission !== 'denied',
);
const notificationCaption = computed(() => {
  if (notificationLoading.value) return '正在检查';
  if (!notificationState.value.supported) return '当前浏览器或连接不支持';
  if (!notificationState.value.available) return '服务端不可用';
  if (notificationState.value.permission === 'denied') return '权限已被浏览器阻止';
  return notificationState.value.enabled ? '已开启' : '未开启';
});

async function refreshNotifications() {
  notificationLoading.value = true;
  notificationError.value = '';
  try {
    notificationState.value = await getPushNotificationState();
  } catch {
    notificationError.value = '无法加载通知设置';
  } finally {
    notificationLoading.value = false;
  }
}

async function setNotificationsEnabled(enabled: boolean) {
  notificationSaving.value = true;
  notificationError.value = '';
  try {
    if (enabled) {
      notificationState.value = await enablePushNotifications();
    } else {
      await disablePushNotifications();
      notificationState.value = await getPushNotificationState();
    }
  } catch {
    notificationError.value = enabled ? '无法开启系统通知' : '无法关闭系统通知';
    await refreshNotifications();
  } finally {
    notificationSaving.value = false;
  }
}

async function refreshAppearance() {
  appearanceLoading.value = true;
  appearanceError.value = '';
  try {
    const settings = await getAppearanceSettings({ notify: false });
    wallpaperColorScheme.value = settings.wallpaperColorScheme;
    persistedWallpaperColorScheme.value = settings.wallpaperColorScheme;
    setWallpaperColorScheme(settings.wallpaperColorScheme);
  } catch {
    appearanceError.value = '无法加载外观设置';
  } finally {
    appearanceLoading.value = false;
  }
}

async function saveWallpaperColorScheme(scheme: WallpaperColorScheme) {
  if (scheme === persistedWallpaperColorScheme.value) return;
  appearanceSaving.value = true;
  appearanceError.value = '';
  try {
    const settings = await updateAppearanceSettings(scheme);
    wallpaperColorScheme.value = settings.wallpaperColorScheme;
    persistedWallpaperColorScheme.value = settings.wallpaperColorScheme;
    setWallpaperColorScheme(settings.wallpaperColorScheme);
  } catch {
    wallpaperColorScheme.value = persistedWallpaperColorScheme.value;
    appearanceError.value = '无法保存外观设置';
  } finally {
    appearanceSaving.value = false;
  }
}

function openProjectOverview(projectId: string) {
  emit('update:modelValue', false);
  void router.push({ name: 'overview', query: { projectId } });
}

function openProjectSettings(project: ProjectSummary) {
  settingsProject.value = project;
  projectSettingsOpen.value = true;
}

function openWorkflowConfig(projectId: string) {
  emit('update:modelValue', false);
  void router.push({ name: 'workflow-config', params: { projectId } });
}

function confirmRemoveProject(projectId: string, projectName: string) {
  removingProjectId.value = projectId;
  removingProjectName.value = projectName;
  removeProjectDialogOpen.value = true;
}

async function removeSelectedProject() {
  if (!removingProjectId.value) return;
  const projectId = removingProjectId.value;
  removingProject.value = true;
  try {
    await removeProjectById(projectId);
    removeProjectDialogOpen.value = false;
    if (route.query.projectId === projectId || route.params.projectId === projectId) {
      emit('update:modelValue', false);
      await router.push({ name: 'overview' });
    }
  } finally {
    removingProject.value = false;
  }
}

function startAdd() {
  adding.value = true;
  void nextTick(() => commandInputRef.value?.focus());
}

function cancelAdd() {
  adding.value = false;
  draftCommand.value = '';
}

async function saveCommand() {
  if (!draftCommand.value.trim()) return;
  saving.value = true;
  try {
    await addQuickCommand(draftCommand.value);
    cancelAdd();
  } catch {
    return;
  } finally {
    saving.value = false;
  }
}

async function removeCommand(id: string) {
  deletingCommandIds.value = [...deletingCommandIds.value, id];
  try {
    await deleteQuickCommand(id);
  } catch {
    return;
  } finally {
    deletingCommandIds.value = deletingCommandIds.value.filter((commandID) => commandID !== id);
  }
}

function refreshQuickCommands() {
  void loadQuickCommands({ force: true }).catch(() => undefined);
}

function changeQuickCommandPage(page: number) {
  void loadQuickCommands({ force: true, page }).catch(() => undefined);
}

onMounted(() => {
  void loadProjects();
});

watch(activeSection, (section) => {
  if (section === 'appearance' && props.modelValue) void refreshAppearance();
  if (section === 'notifications' && props.modelValue) void refreshNotifications();
  if (section !== 'quick_commands' || !props.modelValue) return;
  refreshQuickCommands();
});

watch(
  () => props.modelValue,
  (open) => {
    if (!open) return;
    void loadProjects();
    if (activeSection.value === 'appearance') void refreshAppearance();
    if (activeSection.value === 'notifications') void refreshNotifications();
    if (activeSection.value === 'quick_commands') refreshQuickCommands();
  },
);
</script>
