<template>
  <div class="diff-text-content">
    <div class="diff-text-content__toolbar">
      <span class="text-caption text-muted">{{
        language === 'markdown' ? '文本 · Markdown' : `代码 · ${language}`
      }}</span>
      <ContentViewToggle v-model="view" />
    </div>
    <pre
      v-if="view === 'source'"
      class="diff-text-content__source"
    ><template v-for="(line, index) in patchLines" :key="index">{{ line.prefix }}<span :data-annotation-text="line.line !== undefined ? '' : undefined" :data-annotation-line="line.line" :data-annotation-revision="line.revision">{{ line.text }}</span>{{ index < patchLines.length - 1 ? '\n' : '' }}</template></pre>
    <template v-else>
      <section
        v-for="(lines, index) in sections"
        :key="fileDiff.hunks[index]!.id"
        class="diff-text-content__hunk"
      >
        <div class="diff-text-content__range text-caption text-muted">
          {{ fileDiff.hunks[index]!.header }}
        </div>
        <q-btn
          v-if="fileDiff.hunks[index]!.canExpandBefore"
          flat
          dense
          no-caps
          icon="expand_less"
          label="向上展开 20 行"
          @click="$emit('expand', 'before')"
        />
        <template v-if="language === 'markdown'">
          <div
            v-for="(block, blockIndex) in markdownPreviewBlocks(lines)"
            :key="blockIndex"
            class="diff-text-content__block"
            :class="`diff-text-content__block--${block.kind}`"
          >
            <span
              v-if="block.kind !== 'context'"
              class="diff-text-content__sign"
              :aria-label="block.kind === 'add' ? '新增内容' : '删除内容'"
              >{{ block.kind === 'add' ? '+' : '−' }}</span
            >
            <CodeContent
              v-if="block.lines"
              :text="block.text"
              :language="block.language ?? 'plaintext'"
              :lines="block.lines"
              :toggleable="false"
            />
            <MarkdownContent v-else :text="block.text" :toggleable="false" />
          </div>
        </template>
        <CodeContent
          v-else
          :text="lines.map((line) => line.content).join('\n')"
          :language="language"
          :lines="lines"
          :toggleable="false"
        />
        <q-btn
          v-if="fileDiff.hunks[index]!.canExpandAfter"
          flat
          dense
          no-caps
          icon="expand_more"
          label="向下展开 20 行"
          @click="$emit('expand', 'after')"
        />
      </section>
    </template>
  </div>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue';
import ContentViewToggle from '@/components/ContentViewToggle.vue';
import CodeContent from '@/components/CodeContent.vue';
import MarkdownContent from '@/components/MarkdownContent.vue';
import {
  contentLanguage,
  diffPatchLines,
  diffPreviewSections,
  markdownPreviewBlocks,
} from '@/services/contentPreview';
import type { FileDiff } from '@/services/diff';
const props = defineProps<{ fileDiff: FileDiff; initialSource?: boolean }>();
defineEmits<{ expand: [direction: 'before' | 'after'] }>();
const view = ref<'preview' | 'source'>(props.initialSource ? 'source' : 'preview');
const language = computed(() => contentLanguage(props.fileDiff.file.path));
const sections = computed(() => diffPreviewSections(props.fileDiff));
const patchLines = computed(() => diffPatchLines(props.fileDiff));
</script>
<style scoped>
.diff-text-content {
  min-width: 0;
  padding: 12px;
}
.diff-text-content__toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}
.diff-text-content__source {
  margin: 0;
  padding: 12px;
  max-width: 100%;
  overflow: auto;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface-muted);
  color: var(--ac-text);
  font:
    12px/1.7 'Fira Code',
    'JetBrains Mono',
    monospace;
  white-space: pre;
}
.diff-text-content__hunk {
  min-width: 0;
}
.diff-text-content__hunk + .diff-text-content__hunk {
  margin-top: 18px;
}
.diff-text-content__range {
  padding: 5px 0 10px;
  overflow-wrap: anywhere;
}
.diff-text-content__block {
  position: relative;
  min-width: 0;
  margin: 0 0 8px;
}
.diff-text-content__block--add,
.diff-text-content__block--delete {
  padding: 8px 10px 8px 28px;
  border-left: 3px solid;
  border-radius: 0 4px 4px 0;
}
.diff-text-content__block--add {
  border-color: var(--ac-preview-added-text);
  background: var(--ac-preview-added-bg);
}
.diff-text-content__block--delete {
  border-color: var(--ac-preview-removed-text);
  background: var(--ac-preview-removed-bg);
}
.diff-text-content__block--delete :deep(.markdown-content) {
  color: var(--ac-preview-removed-text);
}
.diff-text-content__block--add :deep(.markdown-content) {
  color: var(--ac-preview-added-text);
}
.diff-text-content__sign {
  position: absolute;
  left: 9px;
  top: 9px;
  font: 600 12px/1.7 monospace;
  user-select: none;
}
</style>
