<template>
  <div class="markdown-content" @click="openResource" v-html="html" />
</template>

<script setup lang="ts">
import DOMPurify from 'dompurify';
import { marked } from 'marked';
import { computed } from 'vue';

import {
  parseSessionEventResourceReference,
  useSessionEventResourceOpener,
} from '@/services/sessionEventResources';

const props = defineProps<{ text: string }>();
const resourceOpener = useSessionEventResourceOpener();

const html = computed(() => {
  const sanitized = DOMPurify.sanitize(marked.parse(props.text || '', { async: false }), {
    ALLOWED_TAGS: [
      'p',
      'br',
      'strong',
      'em',
      'del',
      'blockquote',
      'ul',
      'ol',
      'li',
      'h1',
      'h2',
      'h3',
      'h4',
      'h5',
      'h6',
      'pre',
      'code',
      'a',
      'table',
      'thead',
      'tbody',
      'tr',
      'th',
      'td',
      'hr',
      'img',
    ],
    ALLOWED_ATTR: ['href', 'title', 'src', 'alt'],
  });
  if (!resourceOpener || typeof document === 'undefined') return sanitized;

  const template = document.createElement('template');
  template.innerHTML = sanitized;
  for (const anchor of template.content.querySelectorAll<HTMLAnchorElement>('a[href]')) {
    if (!parseSessionEventResourceReference(anchor.getAttribute('href') ?? '', '')) continue;
    anchor.classList.add('markdown-content__resource-link');
  }
  for (const image of template.content.querySelectorAll<HTMLImageElement>('img[src]')) {
    const reference = image.getAttribute('src') ?? '';
    if (!parseSessionEventResourceReference(reference, '')) continue;
    const anchor = document.createElement('a');
    anchor.href = reference;
    anchor.className = 'markdown-content__resource-link';
    anchor.dataset.eventResource = reference;
    anchor.textContent = image.alt || '查看图片';
    anchor.title = image.title;
    image.replaceWith(anchor);
  }
  return template.innerHTML;
});

function openResource(event: MouseEvent) {
  if (!resourceOpener || !(event.target instanceof Element)) return;
  const target = event.target.closest<HTMLElement>('[data-event-resource], a[href], img[src]');
  if (
    !target ||
    !(event.currentTarget instanceof Element) ||
    !event.currentTarget.contains(target)
  ) {
    return;
  }
  const reference =
    target.dataset.eventResource || target.getAttribute('href') || target.getAttribute('src') || '';
  if (!resourceOpener(reference, target.textContent?.trim() || target.getAttribute('alt') || '')) {
    return;
  }
  event.preventDefault();
  event.stopPropagation();
}
</script>

<style scoped>
.markdown-content {
  min-width: 0;
  color: var(--ac-text);
  font-size: 14px;
  line-height: 1.72;
  overflow-wrap: anywhere;
}

.markdown-content :deep(p),
.markdown-content :deep(ul),
.markdown-content :deep(ol),
.markdown-content :deep(pre),
.markdown-content :deep(blockquote) {
  margin: 0 0 8px;
}

.markdown-content :deep(:last-child) {
  margin-bottom: 0;
}

.markdown-content :deep(ul),
.markdown-content :deep(ol) {
  padding-left: 20px;
}

.markdown-content :deep(code) {
  padding: 1px 4px;
  border-radius: 4px;
  background: var(--ac-surface-muted);
  font-family: 'Fira Code', 'JetBrains Mono', monospace;
  font-size: 0.92em;
}

.markdown-content :deep(pre) {
  overflow: auto;
  padding: 8px 10px;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface-muted);
  white-space: pre;
}

.markdown-content :deep(pre code) {
  padding: 0;
  background: transparent;
}

.markdown-content :deep(img) {
  max-width: 100%;
  height: auto;
}

.markdown-content :deep(.markdown-content__resource-link) {
  color: var(--q-primary);
  cursor: pointer;
}

.markdown-content :deep(.markdown-content__resource-link:focus-visible) {
  border-radius: 4px;
  outline: 2px solid var(--q-primary);
  outline-offset: 2px;
}

.markdown-content :deep(table) {
  display: block;
  max-width: 100%;
  overflow-x: auto;
  border-collapse: collapse;
}
</style>
