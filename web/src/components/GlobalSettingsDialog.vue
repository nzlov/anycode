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
        <q-tab name="codex" icon="memory" label="Codex" />
        <q-tab name="writable_roots" icon="folder_open" label="白名单目录" />
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
              :active="activeSection === 'codex'"
              active-class="global-settings-nav__active"
              @click="activeSection = 'codex'"
            >
              <q-item-section avatar>
                <q-icon name="memory" />
              </q-item-section>
              <q-item-section>Codex</q-item-section>
            </q-item>
            <q-item
              clickable
              :active="activeSection === 'writable_roots'"
              active-class="global-settings-nav__active"
              @click="activeSection = 'writable_roots'"
            >
              <q-item-section avatar>
                <q-icon name="folder_open" />
              </q-item-section>
              <q-item-section>白名单目录</q-item-section>
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
                  <q-icon name="keyboard_return" color="primary" />
                </q-item-section>
                <q-item-section>
                  <q-item-label>发送快捷键</q-item-label>
                  <q-item-label caption>未选中的回车组合用于换行</q-item-label>
                </q-item-section>
                <q-item-section side class="appearance-settings-list__control">
                  <q-select
                    v-model="general.sendShortcut"
                    outlined
                    dense
                    emit-value
                    map-options
                    options-dense
                    :options="sendShortcutOptions"
                    aria-label="发送快捷键"
                    :disable="generalLoading || generalSaving"
                  />
                </q-item-section>
              </q-item>
            </q-list>

            <q-card flat bordered class="general-settings-group">
              <q-item>
                <q-item-section avatar>
                  <q-icon name="hub" color="primary" />
                </q-item-section>
                <q-item-section>
                  <q-item-label>项目思维图</q-item-label>
                  <q-item-label caption>允许项目启用 Agent 维护的隔离思维图</q-item-label>
                </q-item-section>
                <q-item-section side>
                  <q-toggle
                    v-model="general.mindMapEnabled"
                    color="primary"
                    aria-label="项目思维图"
                    :disable="generalLoading || generalSaving"
                  />
                </q-item-section>
              </q-item>
              <q-slide-transition>
                <div v-if="general.mindMapEnabled">
                  <q-separator />
                  <q-list separator class="appearance-settings-list">
                    <q-item>
                      <q-item-section>
                        <q-item-label>默认布局</q-item-label>
                        <q-item-label caption>进入思维图时使用，浏览时可临时切换</q-item-label>
                      </q-item-section>
                      <q-item-section side class="appearance-settings-list__control">
                        <q-select
                          v-model="general.mindMapLayout"
                          outlined
                          dense
                          emit-value
                          map-options
                          options-dense
                          :options="mindMapLayoutOptions"
                          aria-label="思维图默认布局"
                          :disable="generalLoading || generalSaving"
                        />
                      </q-item-section>
                    </q-item>
                    <q-item>
                      <q-item-section>
                        <q-item-label>维护模式</q-item-label>
                        <q-item-label caption
                          >实时由卡片 Agent 维护；异步在会话关闭后排队整理</q-item-label
                        >
                      </q-item-section>
                      <q-item-section side class="appearance-settings-list__control">
                        <q-select
                          v-model="general.mindMapMode"
                          outlined
                          dense
                          emit-value
                          map-options
                          options-dense
                          :options="mindMapModeOptions"
                          aria-label="思维图维护模式"
                          :disable="generalLoading || generalSaving"
                        />
                      </q-item-section>
                    </q-item>
                    <q-item v-if="general.mindMapMode === 'async'" class="column items-stretch">
                      <q-item-section>
                        <q-item-label>异步整理 Agent</q-item-label>
                        <q-item-label caption
                          >所有项目共用此模型、思考强度与任务并发池</q-item-label
                        >
                      </q-item-section>
                      <q-item-section class="q-mt-sm mind-map-agent-settings">
                        <CodexModelSelector
                          v-model:model="general.mindMapModel"
                          v-model:effort="general.mindMapReasoningEffort"
                          :disabled="generalLoading || generalSaving"
                        />
                        <q-input
                          v-model.number="general.mindMapMaxConcurrent"
                          outlined
                          dense
                          type="number"
                          min="1"
                          step="1"
                          label="全局并发任务数"
                          hide-bottom-space
                          aria-label="思维图全局并发任务数"
                          :disable="generalLoading || generalSaving"
                          :error="!mindMapMaxConcurrentValid"
                        />
                      </q-item-section>
                    </q-item>
                  </q-list>
                </div>
              </q-slide-transition>
            </q-card>

            <q-card flat bordered class="general-settings-group general-thinking-settings">
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

        <section v-else-if="activeSection === 'codex'" class="global-settings-panel">
          <q-banner v-if="codexError" dense class="quick-command-error">
            <template #avatar>
              <q-icon name="error_outline" color="negative" />
            </template>
            {{ codexError }}
            <template #action>
              <q-btn
                flat
                round
                dense
                class="app-icon-btn"
                icon="refresh"
                aria-label="重试加载 Codex 设置"
                @click="refreshCodexSettings"
              >
                <q-tooltip>重试</q-tooltip>
              </q-btn>
            </template>
          </q-banner>

          <q-linear-progress v-if="codexLoading || codexSaving" indeterminate color="primary" />
          <q-list bordered separator class="appearance-settings-list">
            <q-item>
              <q-item-section avatar>
                <q-icon name="data_object" color="primary" />
              </q-item-section>
              <q-item-section>
                <q-item-label>上下文长度</q-item-label>
                <q-item-label caption
                  >Codex 可用的上下文 Token 数；留空时跟随模型默认值</q-item-label
                >
              </q-item-section>
              <q-item-section side class="appearance-settings-list__control">
                <q-input
                  v-model="codexContextWindowInput"
                  outlined
                  dense
                  type="number"
                  min="1"
                  max="2147483647"
                  step="1"
                  clearable
                  hide-bottom-space
                  aria-label="Codex 上下文长度"
                  :disable="codexLoading || codexSaving"
                  :error="!codexContextWindowValid"
                  error-message="请输入正整数，或留空跟随模型默认值"
                />
              </q-item-section>
            </q-item>
            <q-item>
              <q-item-section avatar>
                <q-icon name="compress" color="primary" />
              </q-item-section>
              <q-item-section>
                <q-item-label>自动压缩阈值</q-item-label>
                <q-item-label caption
                  >达到此 Token 数时自动压缩；留空时跟随 Codex 默认值</q-item-label
                >
              </q-item-section>
              <q-item-section side class="appearance-settings-list__control">
                <q-input
                  v-model="codexAutoCompactTokenLimitInput"
                  outlined
                  dense
                  type="number"
                  min="1"
                  max="2147483647"
                  step="1"
                  clearable
                  hide-bottom-space
                  aria-label="Codex 自动压缩阈值"
                  :disable="codexLoading || codexSaving"
                  :error="!codexAutoCompactTokenLimitValid"
                  error-message="请输入正整数，或留空跟随 Codex 默认值"
                />
              </q-item-section>
            </q-item>
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
                  v-model.number="codexAgentMaxConcurrent"
                  outlined
                  dense
                  type="number"
                  min="1"
                  step="1"
                  hide-bottom-space
                  aria-label="Agent 并发数量"
                  :disable="codexLoading || codexSaving"
                  :error="!codexAgentMaxConcurrentValid"
                />
              </q-item-section>
            </q-item>
          </q-list>
          <div class="row justify-end q-mt-md">
            <q-btn
              color="primary"
              icon="save"
              label="保存"
              no-caps
              :loading="codexSaving"
              :disable="
                codexLoading ||
                codexSaving ||
                !codexContextWindowValid ||
                !codexAutoCompactTokenLimitValid ||
                !codexAgentMaxConcurrentValid ||
                !codexSettingsChanged
              "
              @click="saveCodexSettings"
            />
          </div>
        </section>

        <section
          v-else-if="activeSection === 'writable_roots'"
          class="global-settings-panel global-settings-panel--writable-roots"
        >
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
                aria-label="重试加载白名单目录"
                @click="refreshGeneralSettings"
              >
                <q-tooltip>重试</q-tooltip>
              </q-btn>
            </template>
          </q-banner>

          <q-linear-progress v-if="generalLoading || generalSaving" indeterminate color="primary" />

          <q-slide-transition>
            <WritableRootEditor
              v-if="addingWritableRoot"
              :ref="setWritableRootEditorRef"
              v-model="draftWritableRoot"
              :disabled="generalLoading || generalSaving"
              :error="draftWritableRootError"
              :valid="draftWritableRootValid"
              @choose-directory="openWritableRootPicker"
              @save="saveWritableRoot"
              @cancel="cancelWritableRootEditor"
            />
          </q-slide-transition>

          <q-list
            v-if="agentWritableRoots.length"
            separator
            class="quick-command-list writable-root-list"
          >
            <q-item v-for="(root, index) in agentWritableRoots" :key="root">
              <q-item-section v-if="editingWritableRootIndex === index">
                <WritableRootEditor
                  :ref="setWritableRootEditorRef"
                  v-model="draftWritableRoot"
                  inline
                  :disabled="generalLoading || generalSaving"
                  :error="draftWritableRootError"
                  :valid="draftWritableRootValid"
                  @choose-directory="openWritableRootPicker"
                  @save="saveWritableRoot"
                  @cancel="cancelWritableRootEditor"
                />
              </q-item-section>
              <template v-else>
                <q-item-section avatar>
                  <q-icon name="folder" color="primary" />
                </q-item-section>
                <q-item-section>
                  <q-item-label class="mono writable-root-path">{{ root }}</q-item-label>
                </q-item-section>
                <q-item-section side>
                  <div class="quick-command-item__actions">
                    <q-btn
                      flat
                      round
                      dense
                      class="app-icon-btn"
                      icon="edit"
                      :aria-label="`修改白名单目录：${root}`"
                      :disable="generalLoading || generalSaving"
                      @click="startEditWritableRoot(index)"
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
                      :aria-label="`删除白名单目录：${root}`"
                      :disable="generalLoading || generalSaving"
                      @click="removeWritableRoot(index)"
                    >
                      <q-tooltip>删除</q-tooltip>
                    </q-btn>
                  </div>
                </q-item-section>
              </template>
            </q-item>
          </q-list>
          <div v-else class="global-settings-empty">
            <q-spinner v-if="generalLoading" color="primary" size="24px" />
            <template v-else>暂无白名单目录</template>
          </div>

          <q-btn
            v-if="!addingWritableRoot && editingWritableRootIndex === null"
            fab
            color="primary"
            class="global-settings-add-fab app-on-primary"
            icon="add"
            aria-label="新增白名单目录"
            :disable="generalLoading || generalSaving"
            @click="startAddWritableRoot"
          >
            <q-tooltip>新增白名单目录</q-tooltip>
          </q-btn>
        </section>

        <section v-else-if="activeSection === 'appearance'" class="global-settings-panel">
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

        <section v-else class="global-settings-panel global-settings-panel--quick-commands">
          <QuickCommandManager v-if="modelValue" hide-header />
        </section>
      </div>
    </q-card>
  </component>

  <ProjectDirectoryDialog
    v-model="directoryPickerOpen"
    select-only
    :initial-path="directoryPickerInitialPath"
    @select="selectWritableRoot"
  />
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import type { VNodeRef } from 'vue';
import { QDialog } from 'quasar';

import CodexModelSelector from '@/components/CodexModelSelector.vue';
import ProjectDirectoryDialog from '@/components/ProjectDirectoryDialog.vue';
import QuickCommandManager from '@/components/QuickCommandManager.vue';
import WritableRootEditor from '@/components/WritableRootEditor.vue';
import { useGeneralSettingsInvalidation } from '@/composables/useGeneralSettingsInvalidation';
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
  getCodexSettings,
  type CodexSettings,
  updateCodexSettings,
} from '@/services/codexSettings';
import {
  defaultSendShortcut,
  getGeneralSettings,
  type GeneralSettings,
  mindMapLayoutOptions,
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
const generalSettingsInvalidation = useGeneralSettingsInvalidation();
const activeSection = ref<
  'general' | 'codex' | 'writable_roots' | 'appearance' | 'notifications' | 'quick_commands'
>('general');
const defaultGeneral: GeneralSettings = {
  agentWritableRoots: [],
  sendShortcut: defaultSendShortcut,
  mindMapEnabled: false,
  mindMapMode: 'realtime',
  mindMapLayout: 'radial',
  mindMapModel: '',
  mindMapReasoningEffort: '',
  mindMapMaxConcurrent: 1,
};
const mindMapModeOptions = [
  { label: '实时', value: 'realtime' },
  { label: '异步', value: 'async' },
];
const sendShortcutOptions = [
  { label: 'Enter', value: 'enter' },
  { label: 'Shift+Enter', value: 'shift_enter' },
];
const general = ref<GeneralSettings>({ ...defaultGeneral });
const persistedGeneral = ref<GeneralSettings>({ ...defaultGeneral });
const agentWritableRoots = ref<string[]>([]);
const addingWritableRoot = ref(false);
const editingWritableRootIndex = ref<number | null>(null);
const draftWritableRoot = ref('');
const writableRootEditorRef = ref<{ focus: () => void } | null>(null);
const directoryPickerOpen = ref(false);
const generalLoading = ref(false);
const generalSaving = ref(false);
const generalError = ref('');
const generalSaveDebounceMs = 500;
let generalSaveTimer: ReturnType<typeof setTimeout> | null = null;
const codexLoading = ref(false);
const codexSaving = ref(false);
const codexError = ref('');
const codexContextWindowInput = ref<string | null>('');
const codexAutoCompactTokenLimitInput = ref<string | null>('');
const codexAgentMaxConcurrent = ref(2);
const persistedCodex = ref<CodexSettings>({
  contextWindow: null,
  autoCompactTokenLimit: null,
  agentMaxConcurrent: 2,
});
const parsedCodexContextWindow = computed(() => {
  const input = (codexContextWindowInput.value ?? '').trim();
  if (!input) return null;
  return Number(input);
});
const codexContextWindowValid = computed(() => {
  const contextWindow = parsedCodexContextWindow.value;
  return (
    contextWindow === null ||
    (Number.isInteger(contextWindow) && contextWindow > 0 && contextWindow <= 2147483647)
  );
});
const parsedCodexAutoCompactTokenLimit = computed(() => {
  const input = (codexAutoCompactTokenLimitInput.value ?? '').trim();
  if (!input) return null;
  return Number(input);
});
const codexAutoCompactTokenLimitValid = computed(() => {
  const autoCompactTokenLimit = parsedCodexAutoCompactTokenLimit.value;
  return (
    autoCompactTokenLimit === null ||
    (Number.isInteger(autoCompactTokenLimit) &&
      autoCompactTokenLimit > 0 &&
      autoCompactTokenLimit <= 2147483647)
  );
});
const codexAgentMaxConcurrentValid = computed(
  () => Number.isInteger(codexAgentMaxConcurrent.value) && codexAgentMaxConcurrent.value > 0,
);
const codexSettingsChanged = computed(
  () =>
    codexContextWindowValid.value &&
    codexAutoCompactTokenLimitValid.value &&
    codexAgentMaxConcurrentValid.value &&
    (parsedCodexContextWindow.value !== persistedCodex.value.contextWindow ||
      parsedCodexAutoCompactTokenLimit.value !== persistedCodex.value.autoCompactTokenLimit ||
      codexAgentMaxConcurrent.value !== persistedCodex.value.agentMaxConcurrent),
);
const agentWritableRootsValid = computed(() =>
  agentWritableRoots.value.every((root) => root.startsWith('/') && root !== '/'),
);
const normalizedDraftWritableRoot = computed(() => draftWritableRoot.value.trim());
const draftWritableRootError = computed(() => {
  const root = normalizedDraftWritableRoot.value;
  if (!root) return '';
  if (!root.startsWith('/') || root === '/') return '请输入根目录以外的绝对路径';
  const duplicateIndex = agentWritableRoots.value.indexOf(root);
  if (duplicateIndex !== -1 && duplicateIndex !== editingWritableRootIndex.value) {
    return '该目录已在白名单中';
  }
  return '';
});
const draftWritableRootValid = computed(
  () => !!normalizedDraftWritableRoot.value && !draftWritableRootError.value,
);
const directoryPickerInitialPath = computed(() =>
  normalizedDraftWritableRoot.value.startsWith('/') ? normalizedDraftWritableRoot.value : '/',
);
const mindMapMaxConcurrentValid = computed(
  () =>
    Number.isInteger(general.value.mindMapMaxConcurrent) && general.value.mindMapMaxConcurrent > 0,
);
const mindMapSettingsValid = computed(
  () =>
    !general.value.mindMapEnabled ||
    general.value.mindMapMode === 'realtime' ||
    (general.value.mindMapMode === 'async' &&
      !!general.value.mindMapModel &&
      !!general.value.mindMapReasoningEffort &&
      mindMapMaxConcurrentValid.value),
);
const generalSettingsValid = computed(
  () =>
    agentWritableRootsValid.value && mindMapSettingsValid.value,
);
const generalSettingsChanged = computed(
  () =>
    general.value.sendShortcut !== persistedGeneral.value.sendShortcut ||
    general.value.mindMapEnabled !== persistedGeneral.value.mindMapEnabled ||
    general.value.mindMapMode !== persistedGeneral.value.mindMapMode ||
    general.value.mindMapLayout !== persistedGeneral.value.mindMapLayout ||
    general.value.mindMapModel !== persistedGeneral.value.mindMapModel ||
    general.value.mindMapReasoningEffort !== persistedGeneral.value.mindMapReasoningEffort ||
    general.value.mindMapMaxConcurrent !== persistedGeneral.value.mindMapMaxConcurrent ||
    JSON.stringify(agentWritableRoots.value) !==
      JSON.stringify(persistedGeneral.value.agentWritableRoots),
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
    agentWritableRoots.value = [...general.value.agentWritableRoots];
  } catch {
    generalError.value = '无法加载常规设置';
  } finally {
    generalLoading.value = false;
  }
}

async function refreshCodexSettings() {
  codexLoading.value = true;
  codexError.value = '';
  try {
    const settings = await getCodexSettings();
    persistedCodex.value = settings;
    codexContextWindowInput.value = settings.contextWindow?.toString() ?? '';
    codexAutoCompactTokenLimitInput.value = settings.autoCompactTokenLimit?.toString() ?? '';
    codexAgentMaxConcurrent.value = settings.agentMaxConcurrent;
  } catch {
    codexError.value = '无法加载 Codex 设置';
  } finally {
    codexLoading.value = false;
  }
}

async function saveCodexSettings() {
  if (
    !codexContextWindowValid.value ||
    !codexAutoCompactTokenLimitValid.value ||
    !codexAgentMaxConcurrentValid.value ||
    !codexSettingsChanged.value
  )
    return;
  codexSaving.value = true;
  codexError.value = '';
  try {
    const settings = await updateCodexSettings({
      contextWindow: parsedCodexContextWindow.value,
      autoCompactTokenLimit: parsedCodexAutoCompactTokenLimit.value,
      agentMaxConcurrent: codexAgentMaxConcurrent.value,
    });
    persistedCodex.value = settings;
    codexContextWindowInput.value = settings.contextWindow?.toString() ?? '';
    codexAutoCompactTokenLimitInput.value = settings.autoCompactTokenLimit?.toString() ?? '';
    codexAgentMaxConcurrent.value = settings.agentMaxConcurrent;
  } catch {
    codexError.value = '无法保存 Codex 设置或重启 Codex 会话';
  } finally {
    codexSaving.value = false;
  }
}

async function saveGeneralSettings() {
  if (!generalSettingsValid.value || !generalSettingsChanged.value) return;
  generalSaving.value = true;
  generalError.value = '';
  try {
    general.value = await updateGeneralSettings({
      agentWritableRoots: agentWritableRoots.value,
      sendShortcut: general.value.sendShortcut,
      mindMapEnabled: general.value.mindMapEnabled,
      mindMapMode: general.value.mindMapMode,
      mindMapLayout: general.value.mindMapLayout,
      mindMapModel: general.value.mindMapModel,
      mindMapReasoningEffort: general.value.mindMapReasoningEffort,
      mindMapMaxConcurrent: general.value.mindMapMaxConcurrent,
    });
    persistedGeneral.value = { ...general.value };
    agentWritableRoots.value = [...general.value.agentWritableRoots];
    generalSettingsInvalidation?.setSendShortcut(general.value.sendShortcut);
    generalSettingsInvalidation?.invalidate();
  } catch {
    general.value = { ...persistedGeneral.value };
    agentWritableRoots.value = [...persistedGeneral.value.agentWritableRoots];
    generalError.value = '无法保存常规设置';
  } finally {
    generalSaving.value = false;
  }
}

function startAddWritableRoot() {
  editingWritableRootIndex.value = null;
  draftWritableRoot.value = '';
  addingWritableRoot.value = true;
  void nextTick(() => writableRootEditorRef.value?.focus());
}

function startEditWritableRoot(index: number) {
  addingWritableRoot.value = false;
  editingWritableRootIndex.value = index;
  draftWritableRoot.value = agentWritableRoots.value[index] ?? '';
  void nextTick(() => writableRootEditorRef.value?.focus());
}

const setWritableRootEditorRef: VNodeRef = (editor) => {
  writableRootEditorRef.value = editor as { focus: () => void } | null;
};

function cancelWritableRootEditor() {
  addingWritableRoot.value = false;
  editingWritableRootIndex.value = null;
  draftWritableRoot.value = '';
}

function saveWritableRoot() {
  if (!draftWritableRootValid.value) return;
  const roots = [...agentWritableRoots.value];
  if (editingWritableRootIndex.value === null) {
    roots.push(normalizedDraftWritableRoot.value);
  } else {
    roots[editingWritableRootIndex.value] = normalizedDraftWritableRoot.value;
  }
  agentWritableRoots.value = roots;
  cancelWritableRootEditor();
}

function removeWritableRoot(index: number) {
  agentWritableRoots.value = agentWritableRoots.value.filter((_, rootIndex) => rootIndex !== index);
  if (editingWritableRootIndex.value === index) cancelWritableRootEditor();
}

function openWritableRootPicker() {
  directoryPickerOpen.value = true;
}

function selectWritableRoot(path: string) {
  draftWritableRoot.value = path;
  directoryPickerOpen.value = false;
  void nextTick(() => writableRootEditorRef.value?.focus());
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

onMounted(() => {
  if (props.modelValue) void refreshGeneralSettings();
});

watch(activeSection, (section) => {
  if (section === 'codex' && props.modelValue) void refreshCodexSettings();
  if (section === 'appearance' && props.modelValue) void refreshAppearance();
  if (section === 'notifications' && props.modelValue) void refreshNotifications();
});

watch(
  [() => general.value.sendShortcut, agentWritableRoots],
  scheduleGeneralSettingsSave,
);

watch(
  [
    () => general.value.mindMapEnabled,
    () => general.value.mindMapMode,
    () => general.value.mindMapLayout,
    () => general.value.mindMapModel,
    () => general.value.mindMapReasoningEffort,
    () => general.value.mindMapMaxConcurrent,
  ],
  scheduleGeneralSettingsSave,
);

function scheduleGeneralSettingsSave() {
  if (generalSaveTimer !== null) clearTimeout(generalSaveTimer);
  if (generalLoading.value || !generalSettingsChanged.value || !generalSettingsValid.value) return;
  generalSaveTimer = setTimeout(() => {
    generalSaveTimer = null;
    void saveGeneralSettings();
  }, generalSaveDebounceMs);
}

watch(
  () => props.modelValue,
  (open) => {
    if (!open) return;
    if (activeSection.value === 'general') void refreshGeneralSettings();
    if (activeSection.value === 'codex') void refreshCodexSettings();
    if (activeSection.value === 'writable_roots') void refreshGeneralSettings();
    if (activeSection.value === 'appearance') void refreshAppearance();
    if (activeSection.value === 'notifications') void refreshNotifications();
  },
);

onBeforeUnmount(() => {
  if (generalSaveTimer === null) return;
  clearTimeout(generalSaveTimer);
  generalSaveTimer = null;
  void saveGeneralSettings();
});
</script>
