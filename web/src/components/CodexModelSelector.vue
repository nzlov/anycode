<template>
  <div v-if="readonly" class="prompt-config-chip codex-model-chip">
    <span>{{ selectionLabel }}</span>
    <q-tooltip>Codex 模型和思考强度：{{ selectionLabel }}</q-tooltip>
  </div>
  <q-select
    v-else
    :model-value="selectionValue"
    :display-value="selectionLabel"
    :options="selectionOptions"
    :loading="loading"
    :disable="disabled"
    dense
    borderless
    emit-value
    map-options
    options-dense
    hide-dropdown-icon
    aria-label="Codex 模型和思考强度"
    class="compact-select codex-model-select"
    @popup-show="loadOptions"
    @update:model-value="selectOption"
  >
    <template #option="scope">
      <q-item v-bind="scope.itemProps">
        <q-item-section>
          <q-item-label>{{ scope.opt.modelLabel }}</q-item-label>
          <q-item-label caption>
            {{ scope.opt.effortLabel }}
            <template v-if="scope.opt.description"> · {{ scope.opt.description }}</template>
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
          <span v-else>暂无可用模型</span>
        </q-item-section>
      </q-item>
    </template>
    <q-tooltip>Codex 模型和思考强度：{{ selectionLabel }}</q-tooltip>
  </q-select>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';

import {
  type CodexModelOption,
  codexModelLabel,
  normalizeCodexSelection,
  reasoningEffortLabel,
} from '@/components/promptOptions';
import { listCodexModelOptions } from '@/services/codexOptions';

interface SelectionOption {
  label: string;
  value: string;
  model: string;
  effort: string;
  modelLabel: string;
  effortLabel: string;
  description: string;
}

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
const selectionOptions = computed<SelectionOption[]>(() =>
  modelOptions.value.flatMap((model) =>
    model.reasoningEfforts.map((effort) => ({
      label: `${model.label} · ${effort.label}`,
      value: selectionKey(model.value, effort.value),
      model: model.value,
      effort: effort.value,
      modelLabel: model.label,
      effortLabel: effort.label,
      description: effort.description ?? '',
    })),
  ),
);
const selectionValue = computed(() => selectionKey(props.model, props.effort));
const selectionLabel = computed(() => {
  const modelLabel = codexModelLabel(modelOptions.value, props.model);
  const effortLabel = reasoningEffortLabel(modelOptions.value, props.model, props.effort);
  return `${modelLabel} · ${effortLabel}`;
});

function selectionKey(model: string, effort: string) {
  return JSON.stringify([model, effort]);
}

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

function selectOption(value: string) {
  const option = selectionOptions.value.find((item) => item.value === value);
  if (!option) return;
  emit('update:model', option.model);
  emit('update:effort', option.effort);
}

function normalizeSelection() {
  if (modelOptions.value.length === 0) return;
  const normalized = normalizeCodexSelection(modelOptions.value, props.model, props.effort);
  if (normalized.model !== props.model) emit('update:model', normalized.model);
  if (normalized.effort !== props.effort) emit('update:effort', normalized.effort);
}

watch(() => [props.model, props.effort] as const, normalizeSelection);
</script>
