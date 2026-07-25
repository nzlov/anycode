<template>
  <component
    :is="page ? 'div' : QDialog"
    :model-value="page ? undefined : modelValue"
    @update:model-value="page ? undefined : emit('update:modelValue', $event)"
  >
    <q-card class="global-settings-dialog app-content-dialog">
      <q-card-section class="global-settings-header row items-center">
        <div class="text-subtitle1 text-weight-bold">全局设置</div>
        <q-space />
        <q-btn flat round dense class="app-icon-btn" icon="close" aria-label="关闭" @click="close">
          <q-tooltip>关闭</q-tooltip>
        </q-btn>
      </q-card-section>

      <q-separator />

      <q-tabs v-model="activeSection" dense align="left" no-caps class="global-settings-tabs lt-sm">
        <q-tab name="general" icon="tune" label="常规" />
        <q-tab name="appearance" icon="palette" label="外观" />
        <q-tab name="notifications" icon="notifications" label="通知" />
        <q-tab name="quick_commands" icon="bolt" label="快捷指令" />
      </q-tabs>

      <div class="global-settings-grid">
        <nav class="global-settings-nav gt-xs" aria-label="全局设置分类">
          <q-list padding>
            <q-item
              clickable
              :active="activeSection === 'general'"
              active-class="global-settings-nav__active"
              @click="activeSection = 'general'"
            >
              <q-item-section avatar>
                <q-icon name="tune" />
              </q-item-section>
              <q-item-section>常规</q-item-section>
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

        <section v-if="activeSection === 'general'" class="global-settings-panel">
          <div class="global-settings-panel__header">
            <div class="text-subtitle2 text-weight-bold">常规</div>
          </div>

          <q-banner v-if="generalError" dense class="quick-command-error">
            <template #avatar>
              <q-icon name="error_outline" color="negative" />
            </template>
            {{ generalError }}
            <template #action>
              <q-btn
                flat
                round
                dense
                class="app-icon-btn"
                icon="refresh"
                aria-label="重试加载常规设置"
                @click="refreshGeneralSettings"
              >
                <q-tooltip>重试</q-tooltip>
              </q-btn>
            </template>
          </q-banner>

          <q-linear-progress v-if="generalLoading || generalSaving" indeterminate color="primary" />
          <div class="general-settings-content">
            <q-list bordered separator class="appearance-settings-list">
              <q-item>
                <q-item-section avatar>
                  <q-icon name="dynamic_feed" color="primary" />
                </q-item-section>
                <q-item-section>
                  <q-item-label>Agent 并发数量</q-item-label>
                  <q-item-label caption>同时运行的 Codex agent 上限</q-item-label>
                </q-item-section>
                <q-item-section side class="appearance-settings-list__control">
                  <q-input
                    v-model.number="general.agentMaxConcurrent"
                    outlined
                    dense
                    type="number"
                    min="1"
                    step="1"
                    hide-bottom-space
                    aria-label="Agent 并发数量"
                    :disable="generalLoading || generalSaving"
                    :error="!agentMaxConcurrentValid"
                  />
                </q-item-section>
              </q-item>
              <q-item class="column items-stretch">
                <q-item-section>
                  <q-item-label>Agent 目录白名单</q-item-label>
                  <q-item-label caption>每行一个绝对路径，仅对“工作区写入”模式生效</q-item-label>
                </q-item-section>
                <q-item-section class="q-mt-sm">
                  <q-input
                    v-model="agentWritableRootsText"
                    outlined
                    dense
                    type="textarea"
                    autogrow
                    aria-label="Agent 目录白名单"
                    placeholder="/home/anycode/.cache/go-build"
                    :disable="generalLoading || generalSaving"
                    :error="!agentWritableRootsValid"
                    error-message="每行必须是绝对路径"
                  />
                </q-item-section>
              </q-item>
            </q-list>

            <q-card flat bordered class="general-thinking-settings">
              <q-item>
                <q-item-section avatar>
                  <q-icon name="psychology" color="primary" />
                </q-item-section>
                <q-item-section>
                  <q-item-label>思考展示</q-item-label>
                  <q-item-label caption>在运行中卡片与事件流底部显示思考语句</q-item-label>
                </q-item-section>
                <q-item-section side>
                  <q-toggle
                    v-model="thinkingPhrasesEnabled"
                    color="primary"
                    aria-label="思考展示"
                  />
                </q-item-section>
              </q-item>
              <q-slide-transition>
                <div v-if="thinkingPhrasesEnabled">
                  <q-separator />
                  <q-card-section class="general-thinking-settings__body">
                    <div>
                      <div class="text-body2">思考语句类型</div>
                      <div class="text-caption text-muted">选择展示语句的语气</div>
                    </div>
                    <q-select
                      v-model="thinkingPhraseStyle"
                      outlined
                      dense
                      emit-value
                      map-options
                      options-dense
                      :options="sessionThinkingPhraseStyleOptions"
                      aria-label="思考语句类型"
                    />
                  </q-card-section>
                </div>
              </q-slide-transition>
            </q-card>
          </div>
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
                <q-icon name="wallpaper" color="primary" />
              </q-item-section>
              <q-item-section>
                <q-item-label>背景</q-item-label>
              </q-item-section>
              <q-item-section side class="appearance-settings-list__control">
                <q-btn-toggle
                  :model-value="appearance.backgroundType"
                  no-caps
                  dense
                  spread
                  unelevated
                  toggle-color="primary"
                  :options="backgroundTypeOptions"
                  :disable="appearanceBusy"
                  aria-label="背景类型"
                  @update:model-value="selectBackgroundType"
                />
              </q-item-section>
            </q-item>
            <q-item v-if="appearance.backgroundType === 'solid'">
              <q-item-section avatar>
                <q-icon name="palette" color="primary" />
              </q-item-section>
              <q-item-section>
                <q-item-label>色系</q-item-label>
              </q-item-section>
              <q-item-section
                side
                class="appearance-settings-list__control appearance-settings-list__control--wide"
              >
                <div class="traditional-theme-options" role="radiogroup" aria-label="中国传统色系">
                  <q-btn
                    v-for="theme in solidThemeOptions"
                    :key="theme.value"
                    flat
                    no-caps
                    dense
                    :disable="appearanceBusy"
                    :class="[
                      'traditional-theme-option',
                      { 'traditional-theme-option--active': appearance.solidTheme === theme.value },
                    ]"
                    :aria-pressed="appearance.solidTheme === theme.value"
                    @click="saveSolidTheme(theme.value)"
                  >
                    <span
                      class="traditional-theme-option__swatch"
                      :style="{ backgroundColor: theme.color }"
                    />
                    <span>{{ theme.label }}</span>
                  </q-btn>
                </div>
              </q-item-section>
            </q-item>
            <q-item v-if="appearance.backgroundType === 'image'">
              <q-item-section avatar>
                <q-icon name="add_photo_alternate" color="primary" />
              </q-item-section>
              <q-item-section>
                <q-item-label>自定义图片</q-item-label>
                <q-item-label v-if="appearance.wallpaperFilename" caption lines="1">
                  {{ appearance.wallpaperFilename }}
                </q-item-label>
              </q-item-section>
              <q-item-section side class="appearance-settings-list__control">
                <q-file
                  v-model="wallpaperFile"
                  outlined
                  dense
                  accept="image/jpeg,image/png,.jpg,.jpeg,.png"
                  :max-file-size="20 * 1024 * 1024"
                  :disable="appearanceBusy"
                  aria-label="上传背景图片"
                  @update:model-value="uploadWallpaper"
                  @rejected="rejectWallpaper"
                >
                  <template #prepend>
                    <q-icon name="upload" />
                  </template>
                </q-file>
              </q-item-section>
            </q-item>
            <q-item v-if="appearance.backgroundType !== 'solid'">
              <q-item-section avatar>
                <q-icon name="format_color_fill" color="primary" />
              </q-item-section>
              <q-item-section>
                <q-item-label>壁纸选色算法</q-item-label>
              </q-item-section>
              <q-item-section side class="appearance-settings-list__control">
                <q-select
                  :model-value="appearance.wallpaperColorScheme"
                  outlined
                  dense
                  emit-value
                  map-options
                  options-dense
                  :options="wallpaperColorSchemeOptions"
                  :disable="appearanceBusy"
                  aria-label="壁纸选色算法"
                  @update:model-value="saveWallpaperColorScheme"
                />
              </q-item-section>
            </q-item>
            <q-item>
              <q-item-section avatar>
                <q-icon name="contrast" color="primary" />
              </q-item-section>
              <q-item-section>
                <q-item-label>背景遮罩</q-item-label>
              </q-item-section>
              <q-item-section side class="appearance-settings-list__control">
                <div class="background-mask-control">
                  <q-slider
                    v-model="appearance.backgroundMask"
                    :min="0"
                    :max="100"
                    :step="1"
                    label
                    label-always
                    :disable="appearanceBusy"
                    aria-label="背景遮罩透明度"
                    @change="saveBackgroundMask"
                  />
                  <span class="background-mask-control__value"
                    >{{ appearance.backgroundMask }}%</span
                  >
                </div>
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
            <q-item>
              <q-item-section avatar>
                <q-icon name="router" color="primary" />
              </q-item-section>
              <q-item-section>
                <q-item-label>通知代理</q-item-label>
              </q-item-section>
              <q-item-section side class="notification-proxy-control">
                <q-input
                  v-model="notificationProxy"
                  outlined
                  dense
                  clearable
                  clear-value=""
                  hide-bottom-space
                  type="url"
                  placeholder="socks5://127.0.0.1:1080"
                  aria-label="通知代理 URL"
                  :disable="notificationLoading || notificationProxySaving"
                  @keyup.enter="saveNotificationProxy"
                >
                  <template #append>
                    <q-btn
                      flat
                      round
                      dense
                      class="app-icon-btn"
                      icon="save"
                      aria-label="保存通知代理"
                      :loading="notificationProxySaving"
                      :disable="!notificationProxyChanged"
                      @click="saveNotificationProxy"
                    >
                      <q-tooltip>保存</q-tooltip>
                    </q-btn>
                  </template>
                </q-input>
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
            <div v-if="adding || editingCommandId" class="quick-command-editor">
              <q-input
                ref="commandInputRef"
                v-model="draftCommand"
                outlined
                autogrow
                :label="editingCommandId ? '修改快捷指令' : '快捷指令'"
                :disable="saving"
                @keyup.ctrl.enter="saveCommand"
              />
              <div class="quick-command-editor__actions">
                <q-btn
                  flat
                  round
                  class="app-icon-btn"
                  icon="close"
                  :aria-label="editingCommandId ? '取消修改' : '取消新增'"
                  :disable="saving"
                  @click="cancelEditor"
                >
                  <q-tooltip>取消</q-tooltip>
                </q-btn>
                <q-btn
                  unelevated
                  round
                  color="primary"
                  class="app-icon-btn app-on-primary"
                  icon="check"
                  :aria-label="editingCommandId ? '保存快捷指令修改' : '保存快捷指令'"
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
                <div class="row no-wrap q-gutter-xs">
                  <q-btn
                    flat
                    round
                    dense
                    class="app-icon-btn"
                    icon="edit_outline"
                    :aria-label="`修改快捷指令：${command.content}`"
                    :disable="quickCommandsLoading || quickCommandsMutating > 0 || saving"
                    @click="startEdit(command)"
                  >
                    <q-tooltip>修改</q-tooltip>
                  </q-btn>
                  <q-btn
                    flat
                    round
                    dense
                    class="app-icon-btn"
                    color="negative"
                    icon="delete_outline"
                    :aria-label="`删除快捷指令：${command.content}`"
                    :loading="deletingCommandIds.includes(command.id)"
                    :disable="quickCommandsLoading || quickCommandsMutating > 0 || saving"
                    @click="removeCommand(command.id)"
                  >
                    <q-tooltip>删除</q-tooltip>
                  </q-btn>
                </div>
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
            :disable="
              adding || !!editingCommandId || quickCommandsLoading || quickCommandsMutating > 0
            "
            @click="startAdd"
          >
            <q-tooltip>新增快捷指令</q-tooltip>
          </q-btn>
        </section>
      </div>
    </q-card>
  </component>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { QDialog } from 'quasar';

import AppPagination from '@/components/AppPagination.vue';
import { useQuickCommands } from '@/composables/useQuickCommands';
import {
  sessionThinkingPhraseStyleOptions,
  useSessionThinkingPhrases,
} from '@/composables/useSessionThinkingPhrases';
import {
  backgroundTypeOptions,
  getAppearanceSettings,
  type AppearanceBackgroundType,
  type AppearanceSettings,
  updateAppearanceSettings,
  uploadAppearanceWallpaper,
  wallpaperColorSchemeOptions,
} from '@/services/appearanceSettings';
import {
  getGeneralSettings,
  type GeneralSettings,
  updateGeneralSettings,
} from '@/services/generalSettings';
import {
  disablePushNotifications,
  enablePushNotifications,
  getPushNotificationState,
  type PushNotificationState,
  updateWebPushProxy,
} from '@/services/pushNotifications';
import { applyAppearanceSettings } from '@/theme/appearance';
import type { WallpaperColorScheme } from '@/theme/dailyBackgroundModel';
import { solidThemeOptions, type AppearanceSolidTheme } from '@/theme/solidThemes';

const props = defineProps<{
  modelValue: boolean;
  page?: boolean;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
}>();

const { thinkingPhrasesEnabled, thinkingPhraseStyle } = useSessionThinkingPhrases();
const activeSection = ref<'general' | 'appearance' | 'notifications' | 'quick_commands'>('general');
const {
  quickCommands,
  quickCommandsLoading,
  quickCommandsMutating,
  quickCommandsError,
  quickCommandsPageInfo,
  loadQuickCommands,
  addQuickCommand,
  editQuickCommand,
  deleteQuickCommand,
} = useQuickCommands();
const adding = ref(false);
const editingCommandId = ref('');
const draftCommand = ref('');
const saving = ref(false);
const deletingCommandIds = ref<string[]>([]);
const commandInputRef = ref<{ focus: () => void } | null>(null);
const defaultGeneral: GeneralSettings = { agentMaxConcurrent: 2, agentWritableRoots: [] };
const general = ref<GeneralSettings>({ ...defaultGeneral });
const persistedGeneral = ref<GeneralSettings>({ ...defaultGeneral });
const agentWritableRootsText = ref('');
const generalLoading = ref(false);
const generalSaving = ref(false);
const generalError = ref('');
const generalSaveDebounceMs = 500;
let generalSaveTimer: ReturnType<typeof setTimeout> | null = null;
const parsedAgentWritableRoots = computed(() => [
  ...new Set(
    agentWritableRootsText.value
      .split('\n')
      .map((root) => root.trim())
      .filter(Boolean),
  ),
]);
const agentWritableRootsValid = computed(() =>
  parsedAgentWritableRoots.value.every((root) => root.startsWith('/') && root !== '/'),
);
const agentMaxConcurrentValid = computed(
  () => Number.isInteger(general.value.agentMaxConcurrent) && general.value.agentMaxConcurrent > 0,
);
const generalSettingsValid = computed(
  () => agentMaxConcurrentValid.value && agentWritableRootsValid.value,
);
const generalSettingsChanged = computed(
  () =>
    general.value.agentMaxConcurrent !== persistedGeneral.value.agentMaxConcurrent ||
    JSON.stringify(parsedAgentWritableRoots.value) !==
      JSON.stringify(persistedGeneral.value.agentWritableRoots),
);
const quickCommandPageMax = computed(() =>
  Math.max(1, Math.ceil(quickCommandsPageInfo.value.total / quickCommandsPageInfo.value.pageSize)),
);
const appearanceLoading = ref(false);
const appearanceSaving = ref(false);
const appearanceUploading = ref(false);
const appearanceError = ref('');
const defaultAppearance: AppearanceSettings = {
  backgroundType: 'bing',
  solidTheme: 'vermilion',
  backgroundMask: 0,
  wallpaperColorScheme: 'content',
  wallpaperId: '',
  wallpaperFilename: '',
};
const appearance = ref<AppearanceSettings>({ ...defaultAppearance });
const persistedAppearance = ref<AppearanceSettings>({ ...defaultAppearance });
const wallpaperFile = ref<File | null>(null);
const appearanceBusy = computed(
  () => appearanceLoading.value || appearanceSaving.value || appearanceUploading.value,
);
const notificationLoading = ref(false);
const notificationSaving = ref(false);
const notificationProxySaving = ref(false);
const notificationError = ref('');
const notificationProxy = ref('');
const persistedNotificationProxy = ref('');
const notificationState = ref<PushNotificationState>({
  supported: true,
  available: true,
  permission: 'default',
  enabled: false,
  proxyUrl: '',
});
const notificationProxyChanged = computed(
  () => notificationProxy.value.trim() !== persistedNotificationProxy.value,
);
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

async function refreshGeneralSettings() {
  generalLoading.value = true;
  generalError.value = '';
  try {
    general.value = await getGeneralSettings();
    persistedGeneral.value = { ...general.value };
    agentWritableRootsText.value = general.value.agentWritableRoots.join('\n');
  } catch {
    generalError.value = '无法加载常规设置';
  } finally {
    generalLoading.value = false;
  }
}

async function saveGeneralSettings() {
  if (!generalSettingsValid.value || !generalSettingsChanged.value) return;
  generalSaving.value = true;
  generalError.value = '';
  try {
    general.value = await updateGeneralSettings({
      agentMaxConcurrent: general.value.agentMaxConcurrent,
      agentWritableRoots: parsedAgentWritableRoots.value,
    });
    persistedGeneral.value = { ...general.value };
    agentWritableRootsText.value = general.value.agentWritableRoots.join('\n');
  } catch {
    general.value = { ...persistedGeneral.value };
    agentWritableRootsText.value = persistedGeneral.value.agentWritableRoots.join('\n');
    generalError.value = '无法保存常规设置';
  } finally {
    generalSaving.value = false;
  }
}

async function refreshNotifications() {
  notificationLoading.value = true;
  notificationError.value = '';
  try {
    notificationState.value = await getPushNotificationState();
    notificationProxy.value = notificationState.value.proxyUrl;
    persistedNotificationProxy.value = notificationState.value.proxyUrl;
  } catch {
    notificationError.value = '无法加载通知设置';
  } finally {
    notificationLoading.value = false;
  }
}

async function saveNotificationProxy() {
  const proxyURL = notificationProxy.value.trim();
  if (proxyURL === persistedNotificationProxy.value) return;
  notificationProxySaving.value = true;
  notificationError.value = '';
  try {
    const config = await updateWebPushProxy(proxyURL);
    notificationProxy.value = config.proxyUrl;
    persistedNotificationProxy.value = config.proxyUrl;
    notificationState.value.proxyUrl = config.proxyUrl;
  } catch {
    notificationError.value = '无法保存通知代理，请检查 URL';
  } finally {
    notificationProxySaving.value = false;
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
    commitAppearance(settings);
  } catch {
    appearanceError.value = '无法加载外观设置';
  } finally {
    appearanceLoading.value = false;
  }
}

async function saveAppearance(patch: Partial<AppearanceSettings>) {
  const candidate = { ...appearance.value, ...patch };
  appearanceSaving.value = true;
  appearanceError.value = '';
  try {
    commitAppearance(await updateAppearanceSettings(candidate));
  } catch {
    appearance.value = { ...persistedAppearance.value };
    appearanceError.value = '无法保存外观设置';
  } finally {
    appearanceSaving.value = false;
  }
}

function commitAppearance(settings: AppearanceSettings) {
  appearance.value = { ...settings };
  persistedAppearance.value = { ...settings };
  applyAppearanceSettings(settings);
}

function selectBackgroundType(backgroundType: AppearanceBackgroundType) {
  appearance.value.backgroundType = backgroundType;
  if (backgroundType === 'image' && !appearance.value.wallpaperId) return;
  void saveAppearance({ backgroundType });
}

function saveSolidTheme(solidTheme: AppearanceSolidTheme) {
  appearance.value.solidTheme = solidTheme;
  void saveAppearance({ solidTheme });
}

function saveWallpaperColorScheme(scheme: WallpaperColorScheme) {
  appearance.value.wallpaperColorScheme = scheme;
  void saveAppearance({ wallpaperColorScheme: scheme });
}

function saveBackgroundMask(backgroundMask: number) {
  void saveAppearance({ backgroundMask });
}

async function uploadWallpaper(file: File | null) {
  if (!file) return;
  appearanceUploading.value = true;
  appearanceError.value = '';
  try {
    commitAppearance(await uploadAppearanceWallpaper(file));
  } catch {
    appearance.value = { ...persistedAppearance.value };
    appearanceError.value = '无法上传背景图片';
  } finally {
    wallpaperFile.value = null;
    appearanceUploading.value = false;
  }
}

function rejectWallpaper() {
  wallpaperFile.value = null;
  appearanceError.value = '请选择不超过 20 MiB 的 JPEG 或 PNG 图片';
}

function close() {
  emit('update:modelValue', false);
}

function startAdd() {
  editingCommandId.value = '';
  adding.value = true;
  void nextTick(() => commandInputRef.value?.focus());
}

function startEdit(command: { id: string; content: string }) {
  adding.value = false;
  editingCommandId.value = command.id;
  draftCommand.value = command.content;
  void nextTick(() => commandInputRef.value?.focus());
}

function cancelEditor() {
  adding.value = false;
  editingCommandId.value = '';
  draftCommand.value = '';
}

async function saveCommand() {
  if (!draftCommand.value.trim()) return;
  saving.value = true;
  try {
    if (editingCommandId.value) {
      await editQuickCommand(editingCommandId.value, draftCommand.value);
    } else {
      await addQuickCommand(draftCommand.value);
    }
    cancelEditor();
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
  if (props.modelValue) void refreshGeneralSettings();
});

watch(activeSection, (section) => {
  if (section === 'general' && props.modelValue) void refreshGeneralSettings();
  if (section === 'appearance' && props.modelValue) void refreshAppearance();
  if (section === 'notifications' && props.modelValue) void refreshNotifications();
  if (section !== 'quick_commands' || !props.modelValue) return;
  refreshQuickCommands();
});

watch(
  [() => general.value.agentMaxConcurrent, agentWritableRootsText],
  () => {
    if (generalSaveTimer !== null) clearTimeout(generalSaveTimer);
    if (generalLoading.value || !generalSettingsChanged.value || !generalSettingsValid.value) return;
    generalSaveTimer = setTimeout(() => {
      generalSaveTimer = null;
      void saveGeneralSettings();
    }, generalSaveDebounceMs);
  },
);

watch(
  () => props.modelValue,
  (open) => {
    if (!open) return;
    if (activeSection.value === 'general') void refreshGeneralSettings();
    if (activeSection.value === 'appearance') void refreshAppearance();
    if (activeSection.value === 'notifications') void refreshNotifications();
    if (activeSection.value === 'quick_commands') refreshQuickCommands();
  },
);

onBeforeUnmount(() => {
  if (generalSaveTimer === null) return;
  clearTimeout(generalSaveTimer);
  generalSaveTimer = null;
  void saveGeneralSettings();
});
</script>
