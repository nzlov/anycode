<template>
  <q-dialog
    :model-value="modelValue"
    :maximized="$q.screen.lt.sm"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <q-card
      class="agent-profile"
      :class="{ 'app-content-dialog': !$q.screen.lt.sm }"
      aria-label="规范画像"
    >
      <q-card-section class="agent-profile__header row items-center no-wrap q-py-sm">
        <q-icon name="psychology" color="primary" size="24px" class="q-mr-sm" />
        <div class="text-subtitle1 text-weight-bold">规范画像</div>
        <q-space />
        <q-btn
          flat
          round
          dense
          icon="refresh"
          aria-label="刷新规范画像"
          :loading="loading"
          :disable="saving || starting"
          @click="refresh()"
        >
          <q-tooltip>刷新</q-tooltip>
        </q-btn>
        <q-btn v-close-popup flat round dense icon="close" aria-label="关闭规范画像" />
      </q-card-section>
      <q-separator />
      <div class="agent-profile__body" role="region" aria-label="规范画像内容" tabindex="0">
        <q-card-section v-if="error" class="q-pb-none">
          <q-banner dense rounded class="agent-profile__error" role="alert">{{ error }}</q-banner>
        </q-card-section>
        <template v-if="profile">
          <q-card-section>
            <q-form id="agent-profile-configuration" class="q-gutter-y-md" @submit="save">
              <q-toggle v-model="draft.enabled" label="在会话启动时注入规范画像" dense />
              <q-input
                v-model="draft.cron"
                outlined
                dense
                label="自动分析 · cron 表达式"
                placeholder="0 3 * * *"
                hint="分钟 小时 日 月 星期；留空关闭自动分析，星期 0 为周日"
                :error="Boolean(preview && !preview.valid)"
                :error-message="preview?.error"
                autocomplete="off"
              />
              <div class="agent-profile__schedule text-caption" aria-live="polite">
                <template v-if="validating">正在校验表达式…</template>
                <template v-else-if="preview">
                  <div>服务器时区：{{ preview.timezone }}</div>
                  <div v-if="preview.valid">{{ preview.description }}</div>
                  <div v-if="preview.nextRunAt">下次执行：{{ serverTime(preview.nextRunAt) }}</div>
                </template>
              </div>
              <div>
                <div class="text-caption text-secondary q-mb-xs">分析模型与思考强度</div>
                <div class="prompt-config-controls">
                  <CodexModelSelector
                    v-model:model="draft.model"
                    v-model:effort="draft.reasoningEffort"
                    :disabled="saving"
                  />
                </div>
              </div>
            </q-form>
          </q-card-section>
          <q-separator />
          <q-card-section>
            <div class="row items-center q-gutter-sm q-mb-sm">
              <div class="text-subtitle2">对 agent 的要求</div>
              <q-badge v-if="profile.status === 'running'" color="primary" label="正在分析" />
              <q-badge v-else-if="profile.status === 'failed'" color="negative" label="分析失败" />
              <q-badge v-else-if="profile.through" color="positive" label="已生成" />
            </div>
            <div v-if="profile.through" class="text-caption text-secondary q-mb-sm">
              已分析至 {{ serverTime(profile.through) }} · 最近一次覆盖
              {{ profile.sessionCount }} 个会话、{{ profile.messageCount }} 条追加描述
            </div>
            <MarkdownContent v-if="profile.prompt" :text="profile.prompt" />
            <div v-else class="agent-profile__empty text-secondary">
              <q-icon name="psychology_alt" size="36px" class="q-mb-sm" />
              <div>尚未生成规范画像</div>
              <div class="text-caption q-mt-xs">点击“立刻分析”，从会话反馈中提取要求。</div>
            </div>
          </q-card-section>
        </template>
        <q-card-section v-else-if="loading" class="text-secondary" role="status">
          正在读取规范画像…
        </q-card-section>
      </div>
      <template v-if="profile">
        <q-separator />
        <q-card-section class="agent-profile__actions">
          <div class="row items-center q-gutter-sm">
            <q-btn
              form="agent-profile-configuration"
              type="submit"
              color="primary"
              unelevated
              label="保存配置"
              :loading="saving"
              :disable="validating || !preview?.valid"
            />
            <q-btn
              outline
              color="primary"
              label="立刻分析"
              icon="auto_awesome"
              :loading="starting || profile.status === 'running'"
              :disable="saving"
              @click="analyze"
            />
          </div>
          <div class="text-caption text-secondary q-mt-sm">
            立刻分析使用已保存的配置；关闭弹窗后分析仍会继续。
          </div>
          <div v-if="profile.error" class="text-negative text-body2 q-mt-sm" role="alert">
            {{ profile.error }}
          </div>
          <div
            v-if="
              profile.progress.stage &&
              (profile.status === 'running' || profile.status === 'failed')
            "
            class="q-mt-sm"
            role="status"
            aria-live="polite"
          >
            <div class="row items-center justify-between text-caption q-mb-xs">
              <span>
                <template v-if="profile.status === 'failed'">已停止 · </template>
                {{ progressStages[profile.progress.stage] || '正在分析' }}
              </span>
              <span v-if="profile.progress.stage !== 'preparing' || profile.status !== 'running'">
                {{ profile.progress.percent }}%
              </span>
            </div>
            <q-linear-progress
              rounded
              size="6px"
              :value="profile.progress.percent / 100"
              :indeterminate="
                profile.status === 'running' && profile.progress.stage === 'preparing'
              "
              :color="profile.status === 'failed' ? 'negative' : 'primary'"
              aria-label="规范画像分析进度"
            />
            <div
              v-if="profile.progress.stage !== 'preparing'"
              class="text-caption text-secondary q-mt-xs"
            >
              已处理 {{ profile.progress.completedMessages }} /
              {{ profile.progress.totalMessages }} 条追加描述<span
                v-if="profile.status === 'running'"
                >；进度按已完成批次的文本量计算</span
              >
            </div>
          </div>
        </q-card-section>
      </template>
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
import { onBeforeUnmount, reactive, ref, watch } from 'vue';
import { useQuasar } from 'quasar';
import MarkdownContent from '@/components/MarkdownContent.vue';
import CodexModelSelector from '@/components/CodexModelSelector.vue';
import {
  analyzeAgentProfile,
  configureAgentProfile,
  getAgentProfile,
  previewProfileSchedule,
  type AgentProfile,
  type ProfileSchedule,
} from '@/services/agentProfile';

const props = defineProps<{ modelValue: boolean }>();
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>();
const $q = useQuasar();
const profile = ref<AgentProfile | null>(null);
const draft = reactive({ enabled: true, cron: '', model: '', reasoningEffort: '' });
const preview = ref<ProfileSchedule | null>(null);
const loading = ref(false);
const saving = ref(false);
const starting = ref(false);
const validating = ref(false);
const error = ref('');
const progressStages: Record<string, string> = {
  preparing: '读取新增追加描述',
  analyzing: '归纳新增反馈',
  reading_context: '读取需要核实的会话片段',
  refining: '结合上下文完善画像',
  saving: '保存画像',
  completed: '分析完成',
};
let poll: ReturnType<typeof setTimeout> | undefined;
let debounce: ReturnType<typeof setTimeout> | undefined;
let generation = 0;
let previewGeneration = 0;

// Keep the server's RFC3339 offset visible instead of converting to the browser timezone.
function serverTime(value: string) {
  return value.replace('T', ' ').replace(/\.\d+/, '').replace('Z', ' UTC');
}

function showError(err: unknown) {
  error.value = err instanceof Error ? err.message : '请求失败，请重试';
}

function scheduleRefresh() {
  clearTimeout(poll);
  if (props.modelValue && profile.value?.status === 'running') {
    poll = setTimeout(() => {
      void refresh(false);
    }, 2500);
  }
}

async function refresh(resetDraft = false) {
  const current = ++generation;
  loading.value = true;
  try {
    const result = await getAgentProfile();
    if (current !== generation || !props.modelValue) return;
    profile.value = result;
    error.value = '';
    if (resetDraft) {
      Object.assign(draft, {
        enabled: result.enabled,
        cron: result.cron,
        model: result.model,
        reasoningEffort: result.reasoningEffort,
      });
      preview.value = result.schedule;
    }
  } catch (err) {
    if (current === generation) showError(err);
  } finally {
    if (current === generation) {
      loading.value = false;
      scheduleRefresh();
    }
  }
}

async function save() {
  const current = ++generation;
  clearTimeout(poll);
  loading.value = false;
  saving.value = true;
  try {
    const result = await configureAgentProfile({ ...draft });
    if (current !== generation || !props.modelValue) return;
    profile.value = result;
    error.value = '';
    $q.notify({ type: 'positive', message: '规范画像配置已保存' });
    scheduleRefresh();
  } catch (err) {
    showError(err);
  } finally {
    saving.value = false;
    scheduleRefresh();
  }
}

async function analyze() {
  const current = ++generation;
  clearTimeout(poll);
  loading.value = false;
  starting.value = true;
  try {
    const result = await analyzeAgentProfile();
    if (current !== generation || !props.modelValue) return;
    profile.value = result;
    error.value = '';
    scheduleRefresh();
  } catch (err) {
    showError(err);
  } finally {
    starting.value = false;
    scheduleRefresh();
  }
}

watch(
  () => draft.cron,
  (cron) => {
    clearTimeout(debounce);
    const current = ++previewGeneration;
    preview.value = null;
    validating.value = true;
    debounce = setTimeout(() => {
      void previewProfileSchedule(cron)
        .then((result) => {
          if (current === previewGeneration) preview.value = result;
        })
        .catch((err: unknown) => {
          if (current === previewGeneration) showError(err);
        })
        .finally(() => {
          if (current === previewGeneration) validating.value = false;
        });
    }, 350);
  },
);

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      void refresh(true);
    } else {
      generation++;
      previewGeneration++;
      clearTimeout(poll);
      clearTimeout(debounce);
      validating.value = false;
    }
  },
  { immediate: true },
);

onBeforeUnmount(() => {
  generation++;
  previewGeneration++;
  clearTimeout(poll);
  clearTimeout(debounce);
});
</script>

<style scoped lang="scss">
.agent-profile {
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.agent-profile.app-content-dialog {
  width: min(760px, calc(100vw - 24px)) !important;
  max-width: calc(100vw - 24px) !important;
  height: min(820px, calc(100dvh - 48px)) !important;
  max-height: calc(100dvh - 48px) !important;
}
.agent-profile__header,
.agent-profile__actions,
.agent-profile > .q-separator {
  flex: 0 0 auto;
}
.agent-profile__body {
  flex: 1 1 auto;
  min-height: 0;
  min-width: 0;
  overflow: auto;
  overscroll-behavior: contain;
  overflow-wrap: anywhere;
}
.agent-profile__actions {
  padding: 12px 16px;
  overflow-wrap: anywhere;
}
.agent-profile__error {
  background: var(--ac-status-danger-bg);
  color: var(--ac-status-danger-text);
}
.agent-profile__schedule {
  border-left: 2px solid var(--q-primary);
  padding-left: 12px;
}
.agent-profile__empty {
  text-align: center;
  padding: 28px 12px;
}
@media (max-width: 599px) {
  .agent-profile {
    width: 100vw;
    max-width: 100vw;
    height: 100dvh;
    max-height: 100dvh;
    border-radius: 0;
  }
}
</style>
