<template>
  <template v-if="readonly">
    <div class="prompt-config-chip codex-model-chip">
      <span>{{ modelLabel }}</span>
      <q-tooltip>Codex 模型：{{ modelLabel }}</q-tooltip>
    </div>
    <div class="prompt-config-chip codex-effort-chip">
      <span>{{ effortLabel }}</span>
      <q-tooltip>思考强度：{{ effortLabel }}</q-tooltip>
    </div>
  </template>
  <template v-else>
    <q-select
      :model-value="model"
      :display-value="modelLabel"
      :options="modelOptions"
      :loading="loading"
      :disable="disabled"
      dense
      borderless
      emit-value
      map-options
      options-dense
      hide-dropdown-icon
      aria-label="Codex 模型"
      class="compact-select model-select"
      @popup-show="loadOptions"
      @update:model-value="selectModel"
    >
      <template #no-option>
        <q-item>
          <q-item-section class="codex-model-select__status">
            <div v-if="loading" class="row items-center justify-center q-gutter-sm">
              <q-spinner color="primary" size="20px" />
              <span>加载模型目录</span>
            </div>
            <div v-else-if="loadError" class="row items-center justify-between q-gutter-sm">
              <span>模型目录加载失败</span>
              <q-btn
                flat
                round
                dense
                icon="refresh"
                aria-label="重试加载模型目录"
                @click.stop="loadOptions"
              >
                <q-tooltip>重试</q-tooltip>
              </q-btn>
            </div>
            <span v-else>暂无可用模型</span>
          </q-item-section>
        </q-item>
      </template>
      <q-tooltip>Codex 模型：{{ modelLabel }}</q-tooltip>
    </q-select>

    <q-select
      :model-value="effort"
      :display-value="effortLabel"
      :options="effortOptions"
      :loading="loading"
      :disable="disabled"
      dense
      borderless
      emit-value
      map-options
      options-dense
      hide-dropdown-icon
      aria-label="思考强度"
      class="compact-select effort-select"
      @popup-show="loadOptions"
      @update:model-value="selectEffort"
    >
      <template #option="scope">
        <q-item v-bind="scope.itemProps">
          <q-item-section>
            <q-item-label>{{ scope.opt.label }}</q-item-label>
            <q-item-label v-if="scope.opt.description" caption>
              {{ scope.opt.description }}
            </q-item-label>
          </q-item-section>
          <q-item-section v-if="scope.selected" side>
            <q-icon name="check" color="primary" />
          </q-item-section>
        </q-item>
      </template>
      <template #no-option>
        <q-item>
          <q-item-section class="codex-model-select__status">
            <div v-if="loading" class="row items-center justify-center q-gutter-sm">
              <q-spinner color="primary" size="20px" />
              <span>加载模型目录</span>
            </div>
            <div v-else-if="loadError" class="row items-center justify-between q-gutter-sm">
              <span>模型目录加载失败</span>
              <q-btn
                flat
                round
                dense
                icon="refresh"
                aria-label="重试加载模型目录"
                @click.stop="loadOptions"
              >
                <q-tooltip>重试</q-tooltip>
              </q-btn>
            </div>
            <span v-else>暂无可用思考强度</span>
          </q-item-section>
        </q-item>
      </template>
      <q-tooltip>思考强度：{{ effortLabel }}</q-tooltip>
    </q-select>
  </template>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';

import {
  type CodexModelOption,
  codexModelLabel,
  normalizeCodexSelection,
  reasoningEffortLabel,
  reasoningEffortOptionsForModel,
} from '@/components/promptOptions';
import { listCodexModelOptions } from '@/services/codexOptions';

const props = withDefaults(
  defineProps<{
    model: string;
    effort: string;
    disabled?: boolean;
    readonly?: boolean;
  }>(),
  {
    disabled: false,
    readonly: false,
  },
);

const emit = defineEmits<{
  'update:model': [value: string];
  'update:effort': [value: string];
}>();

const modelOptions = ref<CodexModelOption[]>([]);
const loading = ref(false);
const loadError = ref(false);
const effortOptions = computed(() =>
  reasoningEffortOptionsForModel(modelOptions.value, props.model),
);
const modelLabel = computed(() => codexModelLabel(modelOptions.value, props.model));
const effortLabel = computed(() =>
  reasoningEffortLabel(modelOptions.value, props.model, props.effort),
);

async function loadOptions() {
  if (loading.value || modelOptions.value.length > 0) return;
  loading.value = true;
  loadError.value = false;
  try {
    modelOptions.value = await listCodexModelOptions();
    normalizeSelection();
  } catch {
    loadError.value = true;
  } finally {
    loading.value = false;
  }
}

function selectModel(value: string) {
  emit('update:model', value);
}

function selectEffort(value: string) {
  emit('update:effort', value);
}

function normalizeSelection() {
  if (modelOptions.value.length === 0) return;
  const normalized = normalizeCodexSelection(modelOptions.value, props.model, props.effort);
  if (normalized.model !== props.model) emit('update:model', normalized.model);
  if (normalized.effort !== props.effort) emit('update:effort', normalized.effort);
}

watch(() => [props.model, props.effort] as const, normalizeSelection);
</script>
