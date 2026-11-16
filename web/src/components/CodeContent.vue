<template>
  <div class="code-content">
    <div v-if="toggleable" class="code-content__toolbar">
      <span>{{ language }}</span
      ><ContentViewToggle v-model="view" />
    </div>
    <pre
      v-if="view === 'source'"
      class="code-content__source"
      data-annotation-text
      data-annotation-line="1"
      >{{ text }}</pre>
    <div v-else class="code-content__lines" tabindex="0" aria-label="代码预览，可横向滚动">
      <div
        v-for="(line, index) in rows"
        :key="index"
        class="code-content__line"
        :class="`code-content__line--${line.kind}`"
      >
        <span class="code-content__number">{{ line.newLine ?? line.oldLine }}</span>
        <span
          class="code-content__sign"
          :aria-label="line.kind === 'add' ? '新增' : line.kind === 'delete' ? '删除' : undefined"
          >{{ line.kind === 'add' ? '+' : line.kind === 'delete' ? '−' : '' }}</span
        >
        <pre
          data-annotation-text
          :data-annotation-line="line.newLine ?? line.oldLine ?? undefined"
          :data-annotation-revision="line.newLine !== null ? 'new' : 'old'"
          v-html="highlighted[index]"
        />
      </div>
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed, ref, shallowRef, watch } from 'vue';
import type { HLJSApi } from 'highlight.js';
import ContentViewToggle from '@/components/ContentViewToggle.vue';
import type { PreviewLine } from '@/services/contentPreview';
const props = withDefaults(
  defineProps<{
    text: string;
    language?: string;
    lines?: PreviewLine[];
    toggleable?: boolean;
    initialSource?: boolean;
  }>(),
  { language: 'plaintext', toggleable: true, initialSource: false },
);
const view = ref<'preview' | 'source'>(props.initialSource ? 'source' : 'preview');
const highlighter = shallowRef<HLJSApi>();
watch(
  () => props.language,
  async () => {
    try {
      highlighter.value = (await import('highlight.js/lib/common')).default;
    } catch {
      /* Source remains readable if highlighting is unavailable. */
    }
  },
  { immediate: true },
);
const rows = computed(
  () =>
    props.lines ??
    props.text
      .split('\n')
      .map((content, i) => ({ content, kind: 'context' as const, oldLine: null, newLine: i + 1 })),
);
const highlighted = computed(() => {
  const hl = highlighter.value;
  if (hl?.getLanguage(props.language))
    return rows.value.map(
      (line) =>
        hl.highlight(line.content, { language: props.language, ignoreIllegals: true }).value,
    );
  return rows.value.map((line) =>
    line.content.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'),
  );
});
</script>
<style scoped>
.code-content {
  min-width: 0;
  color: var(--ac-text);
}
.code-content__toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 8px;
  color: var(--ac-text-muted);
  font-size: 11px;
}
.code-content__lines,
.code-content__source {
  overflow: auto;
  max-width: 100%;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface-muted);
  font:
    12px/1.7 'Fira Code',
    'JetBrains Mono',
    monospace;
}
.code-content__source {
  margin: 0;
  padding: 12px;
  white-space: pre;
}
.code-content__line {
  display: grid;
  grid-template-columns: 42px 20px minmax(max-content, 1fr);
  min-width: max-content;
}
.code-content__line pre {
  margin: 0;
  padding: 2px 12px 2px 0;
  white-space: pre;
  font: inherit;
}
.code-content__number,
.code-content__sign {
  padding: 2px 5px;
  text-align: right;
  color: var(--ac-text-muted);
  user-select: none;
}
.code-content__sign {
  text-align: center;
}
.code-content__line--add {
  background: var(--ac-preview-added-bg);
  color: var(--ac-preview-added-text);
}
.code-content__line--delete {
  background: var(--ac-preview-removed-bg);
  color: var(--ac-preview-removed-text);
}
.code-content :deep(.hljs-comment) {
  color: var(--ac-text-muted);
}
.code-content :deep(.hljs-keyword),
.code-content :deep(.hljs-literal) {
  color: var(--ac-link);
}
.code-content :deep(.hljs-string),
.code-content :deep(.hljs-attr) {
  color: var(--ac-secondary);
}
.code-content :deep(.hljs-number),
.code-content :deep(.hljs-title) {
  color: var(--ac-tertiary);
}
</style>
