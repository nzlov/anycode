<template>
  <q-banner
    v-if="phase === 'after_run' && !normalizedResult"
    dense
    rounded
    class="workflow-result-review__error app-feedback app-feedback--danger"
  >
    执行后审批缺少节点结果，请刷新后重试。结果恢复前不能提交审批。
  </q-banner>
  <div v-else-if="!normalizedResult" class="workflow-result-review__empty text-muted">
    {{ phase === 'before_run' ? '该节点将在运行前等待批准，尚无执行结果。' : '未找到审批结果。' }}
  </div>
  <section v-else class="workflow-result-review" aria-label="节点执行结果">
    <header class="workflow-result-review__summary">
      <q-badge :color="outcomePresentation.color" :label="outcomePresentation.label" />
      <div class="text-body1 text-weight-medium">{{ normalizedResult.summary }}</div>
    </header>

    <section v-if="dataProjection.entries.length" class="workflow-result-review__section">
      <div class="text-subtitle2 text-weight-bold">关键结果</div>
      <dl class="workflow-result-review__data">
        <template v-for="(entry, index) in dataProjection.entries" :key="`${entry.key}:${index}`">
          <dt>{{ entry.key }}</dt>
          <dd>{{ formatValue(entry.value) }}</dd>
        </template>
      </dl>
      <div v-if="dataProjection.truncated" class="text-caption text-warning">
        关键结果内容过深或条目过多，已截断展示。
      </div>
    </section>

    <section v-if="normalizedResult.checks.length" class="workflow-result-review__section">
      <div class="text-subtitle2 text-weight-bold">验证结果</div>
      <q-list separator class="workflow-result-review__list">
        <q-item v-for="(check, index) in normalizedResult.checks" :key="`${check.id}:${index}`" dense>
          <q-item-section avatar>
            <q-icon :name="checkPresentation(check.status).icon" :color="checkPresentation(check.status).color" />
          </q-item-section>
          <q-item-section>
            <q-item-label>{{ check.label }}</q-item-label>
            <q-item-label v-if="check.detail" caption>{{ check.detail }}</q-item-label>
          </q-item-section>
          <q-item-section side>
            <q-badge outline class="text-muted" :label="check.source === 'system' ? '系统验证' : 'Agent 验证'" />
          </q-item-section>
        </q-item>
      </q-list>
    </section>

    <q-banner
      v-if="normalizedResult.warnings.length"
      dense
      class="workflow-result-review__warnings app-feedback app-feedback--warning"
    >
      <div class="text-subtitle2 text-weight-bold">注意事项</div>
      <div v-for="(warning, index) in normalizedResult.warnings" :key="`${warning.code}:${index}`" class="q-mt-xs">{{ warning.message }}</div>
    </q-banner>

    <section v-if="normalizedResult.artifacts.length" class="workflow-result-review__section">
      <div class="text-subtitle2 text-weight-bold">产物</div>
      <q-list dense separator class="workflow-result-review__list">
        <q-item v-for="(artifact, index) in normalizedResult.artifacts" :key="`${artifact.kind}:${artifact.ref}:${index}`">
          <q-item-section>
            <q-item-label>{{ artifact.label }}</q-item-label>
            <q-item-label caption class="text-mono">{{ artifact.ref }}</q-item-label>
          </q-item-section>
          <q-item-section side><q-badge outline class="text-muted" :label="artifact.kind" /></q-item-section>
        </q-item>
      </q-list>
    </section>

    <q-expansion-item dense icon="data_object" label="原始结果" class="workflow-result-review__raw">
      <pre>{{ formattedResult }}</pre>
    </q-expansion-item>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import type { PendingApproval, WorkflowNodeResult } from '@/services/sessions';
import {
  flattenWorkflowResultData,
  normalizeWorkflowNodeResult,
} from '@/services/workflowApprovalReview';

const props = defineProps<{
  phase: PendingApproval['phase'] | null;
  result: WorkflowNodeResult | null;
}>();

const normalizedResult = computed(() => normalizeWorkflowNodeResult(props.result));
const outcomePresentation = computed(() => {
  if (normalizedResult.value?.outcome === 'success') return { label: '成功', color: 'positive' };
  if (normalizedResult.value?.outcome === 'partial') return { label: '部分完成', color: 'warning' };
  return { label: '失败', color: 'negative' };
});
const dataProjection = computed(() => flattenWorkflowResultData(normalizedResult.value?.data ?? {}));
const formattedResult = computed(() => safeJSONStringify(normalizedResult.value, '原始结果层级过深，无法格式化展示。'));

function formatValue(value: unknown) {
  if (value == null) return '-';
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') return String(value);
  return safeJSONStringify(value, '[复杂内容，请查看原始结果]');
}

function safeJSONStringify(value: unknown, fallback: string): string {
  try {
    return JSON.stringify(value, null, 2) ?? fallback;
  } catch {
    return fallback;
  }
}

function checkPresentation(status: WorkflowNodeResult['checks'][number]['status']) {
  if (status === 'passed') return { icon: 'check_circle', color: 'positive' };
  if (status === 'warning') return { icon: 'warning', color: 'warning' };
  return { icon: 'cancel', color: 'negative' };
}
</script>

<style scoped>
.workflow-result-review { display: grid; gap: 16px; min-width: 0; }
.workflow-result-review__empty { padding: 24px 0; text-align: center; }
.workflow-result-review__error { margin: 4px 0; }
.workflow-result-review__summary { display: flex; align-items: flex-start; gap: 10px; }
.workflow-result-review__section { display: grid; gap: 8px; }
.workflow-result-review__data { display: grid; grid-template-columns: minmax(120px, 180px) minmax(0, 1fr); margin: 0; border-top: 1px solid var(--ac-border); }
.workflow-result-review__data dt, .workflow-result-review__data dd { min-width: 0; margin: 0; padding: 8px 0; border-bottom: 1px solid var(--ac-border); overflow-wrap: anywhere; }
.workflow-result-review__data dt { color: var(--ac-text-muted); }
.workflow-result-review__list { border-top: 1px solid var(--ac-border); border-bottom: 1px solid var(--ac-border); }
.workflow-result-review__raw pre { max-height: 320px; margin: 0; padding: 12px; overflow: auto; background: var(--ac-surface-muted); white-space: pre-wrap; overflow-wrap: anywhere; }
@media (max-width: 599.98px) {
  .workflow-result-review__data { grid-template-columns: 1fr; }
  .workflow-result-review__data dt { padding-bottom: 0; border-bottom: 0; }
}
</style>
