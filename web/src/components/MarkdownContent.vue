<template>
  <div ref="contentElement" class="markdown-content" @click="openResource" v-html="html" />
</template>

<script setup lang="ts">
import DOMPurify from 'dompurify';
import { marked } from 'marked';
import { Dark } from 'quasar';
import { computed, nextTick, ref, watch } from 'vue';

import {
  parseSessionEventResourceReference,
  useSessionEventResourceOpener,
} from '@/services/sessionEventResources';

const props = defineProps<{ text: string }>();
const resourceOpener = useSessionEventResourceOpener();
const contentElement = ref<HTMLElement>();

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
      'input',
    ],
    ALLOWED_ATTR: ['href', 'title', 'src', 'alt'],
    ALLOW_DATA_ATTR: false,
    ADD_ATTR: (attribute, tag) =>
      (attribute === 'align' && (tag === 'th' || tag === 'td')) ||
      (attribute === 'start' && tag === 'ol') ||
      (attribute === 'class' && tag === 'code') ||
      (tag === 'input' && ['type', 'checked', 'disabled'].includes(attribute)),
  });
  if (typeof document === 'undefined') return sanitized;

  const template = document.createElement('template');
  template.innerHTML = sanitized;
  for (const input of template.content.querySelectorAll('input')) {
    if (input.type !== 'checkbox') {
      input.remove();
      continue;
    }
    input.disabled = true;
    input.setAttribute('aria-label', input.checked ? '已完成' : '未完成');
  }
  for (const code of template.content.querySelectorAll('code')) {
    const language = /^language-([\w+-]+)$/.exec(code.className)?.[1];
    code.removeAttribute('class');
    if (language) code.dataset.language = language.toLowerCase();
  }
  if (!resourceOpener) return template.innerHTML;
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

watch(
  [html, () => Dark.isActive],
  async ([currentHtml], [previousHtml], onCleanup) => {
    let cancelled = false;
    onCleanup(() => {
      cancelled = true;
    });
    await nextTick();
    if (cancelled) return;
    // Restore code sources before redrawing diagrams after a theme change.
    if (currentHtml === previousHtml && contentElement.value) {
      contentElement.value.innerHTML = currentHtml;
    }
    const blocks = [
      ...(contentElement.value?.querySelectorAll<HTMLElement>('pre > code[data-language]') ?? []),
    ];
    if (!blocks.length) return;
    try {
      const highlighted = blocks.filter((code) => code.dataset.language !== 'mermaid');
      if (highlighted.length) {
        const { default: highlighter } = await import('highlight.js/lib/common');
        if (cancelled) return;
        for (const code of highlighted) {
          const language = code.dataset.language!;
          if (highlighter.getLanguage(language)) {
            code.innerHTML = highlighter.highlight(code.textContent ?? '', {
              language,
              ignoreIllegals: true,
            }).value;
          }
        }
      }
      const diagrams = blocks.filter((code) => code.dataset.language === 'mermaid');
      if (!diagrams.length) return;
      // Wait briefly for streaming text to settle before laying out diagrams.
      await new Promise((resolve) => setTimeout(resolve, 150));
      if (cancelled) return;
      const { default: mermaid } = await import('mermaid');
      if (cancelled) return;
      mermaid.initialize({
        startOnLoad: false,
        securityLevel: 'strict',
        htmlLabels: false,
        theme: Dark.isActive ? 'dark' : 'default',
        suppressErrorRendering: true,
        secure: [
          'securityLevel',
          'startOnLoad',
          'htmlLabels',
          'maxTextSize',
          'maxEdges',
          'suppressErrorRendering',
        ],
      });
      for (const code of diagrams) {
        if (cancelled) return;
        const pre = code.parentElement!;
        const container = document.createElement('div');
        container.style.cssText =
          'position:absolute;visibility:hidden;pointer-events:none;left:0;top:0';
        document.body.append(container);
        try {
          const { svg } = await mermaid.render(
            `markdown-diagram-${crypto.randomUUID()}`,
            code.textContent ?? '',
            container,
          );
          if (cancelled) return;
          const image = document.createElement('img');
          image.alt = 'Mermaid 图表';
          // SVG images isolate diagram styles and links from the surrounding session UI.
          image.src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(DOMPurify.sanitize(svg, { USE_PROFILES: { svg: true, svgFilters: true }, FORBID_TAGS: ['foreignObject'] }))}`;
          const svgRoot = new DOMParser().parseFromString(svg, 'image/svg+xml').documentElement;
          const viewBox = svgRoot
            .getAttribute('viewBox')
            ?.trim()
            .split(/[\s,]+/)
            .map(Number);
          if (viewBox && viewBox.length === 4 && viewBox[2]! > 0 && viewBox[3]! > 0) {
            image.width = Math.ceil(viewBox[2]!);
            image.height = Math.ceil(viewBox[3]!);
          }
          const figure = document.createElement('figure');
          figure.className = 'markdown-content__diagram';
          figure.tabIndex = 0;
          figure.setAttribute('aria-label', 'Mermaid 图表，可横向滚动');
          figure.append(image);
          pre.replaceWith(figure);
        } catch {
          if (!cancelled) pre.dataset.diagramError = '图表语法尚不完整或无效，显示源码';
        } finally {
          container.remove();
        }
      }
    } catch {
      // Keep the readable source if an optional renderer cannot be loaded.
    }
  },
  { immediate: true, flush: 'post' },
);

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

.markdown-content :deep(h1),
.markdown-content :deep(h2),
.markdown-content :deep(h3),
.markdown-content :deep(h4),
.markdown-content :deep(h5),
.markdown-content :deep(h6) {
  margin: 16px 0 8px;
  line-height: 1.35;
}

.markdown-content :deep(h1) {
  font-size: 28px;
}

.markdown-content :deep(h2) {
  font-size: 24px;
}

.markdown-content :deep(h3) {
  font-size: 20px;
}

.markdown-content :deep(h4) {
  font-size: 18px;
}

.markdown-content :deep(h5) {
  font-size: 16px;
}

.markdown-content :deep(h6) {
  font-size: 14px;
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
  margin: 0 0 8px;
  overflow-x: auto;
  border-collapse: collapse;
}

.markdown-content :deep(th),
.markdown-content :deep(td) {
  padding: 6px 12px;
  border: 1px solid var(--ac-border);
  vertical-align: top;
}

.markdown-content :deep(th) {
  background: var(--ac-surface-muted);
  font-weight: 600;
}
.markdown-content :deep(th:not([align])),
.markdown-content :deep(td:not([align])) {
  text-align: left;
}

.markdown-content :deep(blockquote) {
  padding: 4px 12px;
  border-left: 3px solid var(--ac-border-strong);
  background: var(--ac-surface-muted);
  color: var(--ac-text-muted);
}

.markdown-content :deep(input[type='checkbox']) {
  margin: 0 6px 0 0;
  vertical-align: middle;
  accent-color: var(--ac-link);
}

.markdown-content :deep(li:has(> input[type='checkbox'])),
.markdown-content :deep(li:has(> p > input[type='checkbox'])) {
  list-style: none;
}

.markdown-content :deep(.hljs-comment),
.markdown-content :deep(.hljs-quote) {
  color: var(--ac-text-muted);
  font-style: italic;
}

.markdown-content :deep(.hljs-keyword),
.markdown-content :deep(.hljs-selector-tag),
.markdown-content :deep(.hljs-literal),
.markdown-content :deep(.hljs-built_in) {
  color: var(--ac-link);
}

.markdown-content :deep(.hljs-string),
.markdown-content :deep(.hljs-attr),
.markdown-content :deep(.hljs-template-tag) {
  color: var(--ac-secondary);
}

.markdown-content :deep(.hljs-number),
.markdown-content :deep(.hljs-title),
.markdown-content :deep(.hljs-type) {
  color: var(--ac-tertiary);
}

.markdown-content :deep(.markdown-content__diagram) {
  overflow: auto;
  margin: 8px 0;
  padding: 8px;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface-muted);
}

.markdown-content :deep(.markdown-content__diagram img) {
  display: block;
  max-width: none;
}

.markdown-content :deep(.markdown-content__diagram:hover) {
  border-color: var(--ac-border-strong);
}

.markdown-content :deep(.markdown-content__diagram:focus-visible) {
  outline: 2px solid var(--ac-link);
  outline-offset: 2px;
}

.markdown-content :deep(pre[data-diagram-error])::before {
  display: block;
  margin-bottom: 8px;
  color: var(--ac-text-muted);
  font-family: sans-serif;
  white-space: normal;
  content: attr(data-diagram-error);
}
</style>
