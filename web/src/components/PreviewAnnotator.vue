<template>
  <div class="preview-annotator" :class="`preview-annotator--${mode}`">
    <div
      v-if="enabled"
      class="preview-annotator__toolbar"
      role="toolbar"
      aria-label="预览标注工具栏"
    >
      <div class="preview-annotator__toolbar-leading"><slot name="toolbar-leading" /></div>
      <div class="preview-annotator__toolbar-controls">
        <q-btn-dropdown
          v-if="mode === 'image'"
          split
          no-caps
          dense
          unelevated
          color="primary"
          icon="crop_free"
          label="框选"
          :aria-label="`框选（${shapeLabel}）`"
          @click="armShape(shape)"
        >
          <q-list dense>
            <q-item v-close-popup clickable @click="armShape('rectangle')">
              <q-item-section avatar><q-icon name="crop_square" /></q-item-section>
              <q-item-section>矩形</q-item-section>
            </q-item>
            <q-item v-close-popup clickable @click="armShape('ellipse')">
              <q-item-section avatar><q-icon name="circle" /></q-item-section>
              <q-item-section>圆形</q-item-section>
            </q-item>
          </q-list>
        </q-btn-dropdown>
        <q-btn
          v-else
          no-caps
          dense
          unelevated
          color="primary"
          icon="rate_review"
          label="批注选中内容"
          :disable="!pendingTextRange"
          @click="openTextEditor"
        />
        <q-badge v-if="annotations.length" outline color="primary" :label="annotations.length" />
        <q-space />
        <q-btn
          flat
          dense
          no-caps
          icon="add_comment"
          label="注入"
          :disable="annotations.length === 0 || !canInject"
          @click="injectAnnotations"
        >
          <q-tooltip>将当前预览中新建的全部标注添加为批注附件</q-tooltip>
        </q-btn>
        <q-btn
          v-if="annotations.length"
          flat
          round
          dense
          icon="delete_sweep"
          aria-label="清空标注"
          @click="clearAnnotations"
        >
          <q-tooltip>清空一次性标注</q-tooltip>
        </q-btn>
      </div>
      <div class="preview-annotator__toolbar-actions"><slot name="toolbar-actions" /></div>
    </div>

    <div
      ref="surfaceElement"
      class="preview-annotator__surface"
      :class="{ 'preview-annotator__surface--armed': mode === 'image' && armed }"
      @pointerdown="startImageSelection"
      @pointermove="moveImageSelection"
      @pointerup="finishImageSelection"
      @pointercancel="cancelImageSelection"
      @mouseup="captureTextSelection"
      @keyup="captureTextSelection"
    >
      <slot />

      <template v-if="mode === 'image'">
        <div
          v-for="(annotation, index) in renderedImageAnnotations"
          :key="annotation.id"
          class="preview-annotator__shape"
          :class="{
            'preview-annotator__shape--ellipse': annotation.shape === 'ellipse',
            'preview-annotator__shape--selected': selectedId === annotation.id,
          }"
          :style="imageAnnotationStyle(annotation)"
          @pointerdown.stop="selectAnnotation(annotation.id)"
          @dblclick.stop="editImageAnnotation(annotation)"
        >
          <span class="preview-annotator__marker-index">{{ index + 1 }}</span>
          <q-btn
            v-if="!isDisplayAnnotation(annotation.id)"
            round
            dense
            size="xs"
            class="preview-annotator__shape-note"
            icon="edit_note"
            aria-label="编辑备注"
            @click.stop="openImageEditor(annotation)"
          />
          <template v-if="selectedId === annotation.id && !isDisplayAnnotation(annotation.id)">
            <button
              v-for="corner in resizeCorners"
              :key="corner"
              type="button"
              class="preview-annotator__resize-handle"
              :class="`preview-annotator__resize-handle--${corner}`"
              :aria-label="`从${cornerLabel(corner)}调整标注大小`"
              @pointerdown.stop.prevent="startResize($event, annotation, corner)"
            />
          </template>
        </div>
      </template>

      <template v-else>
        <button
          v-for="highlight in textHighlights"
          :key="highlight.key"
          type="button"
          class="preview-annotator__highlight"
          :style="highlight.style"
          :aria-label="`编辑批注：${highlight.annotation.note || highlight.annotation.quote}`"
          @click="editTextAnnotation(highlight.annotation)"
        >
          <span class="preview-annotator__marker-index">{{ highlight.index }}</span>
        </button>
      </template>
    </div>

    <q-dialog v-model="editorOpen" persistent>
      <q-card class="preview-annotator__editor">
        <q-card-section class="text-subtitle2 text-weight-bold">添加备注</q-card-section>
        <q-card-section v-if="editorQuote" class="preview-annotator__quote">
          {{ editorQuote }}
        </q-card-section>
        <q-card-section>
          <q-input
            v-model="editorNote"
            autofocus
            autogrow
            outlined
            label="备注（可选）"
            @keydown.ctrl.enter.prevent="saveEditor"
            @keydown.meta.enter.prevent="saveEditor"
          />
        </q-card-section>
        <q-card-actions align="right">
          <q-btn
            v-if="editorExisting"
            flat
            no-caps
            color="negative"
            label="删除标注"
            @click="deleteEditorAnnotation"
          />
          <q-space />
          <q-btn flat no-caps label="取消" @click="cancelEditor" />
          <q-btn unelevated no-caps color="primary" label="保存" @click="saveEditor" />
        </q-card-actions>
      </q-card>
    </q-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';

import {
  formatPreviewAnnotationDraft,
  type ImageAnnotationShape,
  type ImagePreviewAnnotation,
  type PreviewAnnotation,
  type PreviewFileReference,
  type TextAnnotationPosition,
  type TextPreviewAnnotation,
} from '@/services/previewAnnotations';
import { useAnnotationDraftInjector } from '@/services/annotationDraftInjection';

interface Bounds {
  left: number;
  top: number;
  width: number;
  height: number;
}

interface StoredTextAnnotation extends TextPreviewAnnotation {
  range: Range;
}

interface Highlight {
  key: string;
  annotation: StoredTextAnnotation;
  style: Record<string, string>;
  index: number;
}

type ResizeCorner = 'nw' | 'ne' | 'sw' | 'se';

const props = withDefaults(
  defineProps<{
    mode: 'image' | 'text';
    source: string;
    sessionId?: string;
    contentKey?: unknown;
    fileReferences?: PreviewFileReference[];
    displayAnnotations?: PreviewAnnotation[];
    enabled?: boolean;
  }>(),
  { sessionId: '', fileReferences: () => [], displayAnnotations: () => [], enabled: true },
);

const injector = useAnnotationDraftInjector();
const surfaceElement = ref<HTMLElement | null>(null);
const imageBounds = ref<Bounds>({ left: 0, top: 0, width: 0, height: 0 });
const imageAnnotations = ref<ImagePreviewAnnotation[]>([]);
const textAnnotations = ref<StoredTextAnnotation[]>([]);
const displayTextAnnotations = ref<StoredTextAnnotation[]>([]);
const textHighlights = ref<Highlight[]>([]);
const shape = ref<ImageAnnotationShape>('rectangle');
const armed = ref(false);
const selectedId = ref('');
const pendingTextRange = ref<Range | null>(null);
const editorOpen = ref(false);
const editorNote = ref('');
const editorQuote = ref('');
const editorId = ref('');
const editorExisting = ref(false);
const editorKind = ref<'image' | 'text'>('image');
const resizeCorners: ResizeCorner[] = ['nw', 'ne', 'sw', 'se'];
let draftImage: ImagePreviewAnnotation | null = null;
let drawingStart: { x: number; y: number } | null = null;
let drawingPointer = -1;
let resizeState:
  | {
      pointerId: number;
      annotation: ImagePreviewAnnotation;
      corner: ResizeCorner;
      startX: number;
      startY: number;
      original: ImagePreviewAnnotation;
    }
  | undefined;
let resizeObserver: ResizeObserver | null = null;
let mutationObserver: MutationObserver | null = null;

const annotations = computed<PreviewAnnotation[]>(() => [
  ...imageAnnotations.value,
  ...textAnnotations.value.map((annotation) => ({
    id: annotation.id,
    kind: annotation.kind,
    start: annotation.start,
    end: annotation.end,
    quote: annotation.quote,
    note: annotation.note,
  })),
]);
const displayAnnotationIDs = computed(
  () => new Set(props.displayAnnotations.map((annotation) => annotation.id)),
);
const renderedImageAnnotations = computed(() => [
  ...imageAnnotations.value,
  ...props.displayAnnotations.filter(
    (annotation): annotation is ImagePreviewAnnotation => annotation.kind === 'image',
  ),
]);
const shapeLabel = computed(() => (shape.value === 'ellipse' ? '圆形' : '矩形'));
const canInject = computed(() => injector?.canInject(props.sessionId) ?? false);

function annotationId() {
  return `annotation-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

function armShape(nextShape: ImageAnnotationShape) {
  shape.value = nextShape;
  armed.value = true;
  selectedId.value = '';
}

function startImageSelection(event: PointerEvent) {
  if (props.mode !== 'image' || !armed.value || resizeState) return;
  syncImageBounds();
  const point = normalizedImagePoint(event);
  if (!point) return;
  event.preventDefault();
  event.stopPropagation();
  drawingPointer = event.pointerId;
  surfaceElement.value?.setPointerCapture(event.pointerId);
  const annotation: ImagePreviewAnnotation = {
    id: annotationId(),
    kind: 'image',
    shape: shape.value,
    x: point.x,
    y: point.y,
    width: 0,
    height: 0,
    note: '',
  };
  imageAnnotations.value = [...imageAnnotations.value, annotation];
  draftImage = imageAnnotations.value.at(-1) ?? null;
  drawingStart = point;
}

function moveImageSelection(event: PointerEvent) {
  if (resizeState?.pointerId === event.pointerId) {
    resizeAnnotation(event);
    return;
  }
  if (!draftImage || drawingPointer !== event.pointerId) return;
  event.preventDefault();
  event.stopPropagation();
  const point = normalizedImagePoint(event, true);
  if (!point) return;
  const startX = drawingStart?.x ?? draftImage.x;
  const startY = drawingStart?.y ?? draftImage.y;
  draftImage.x = Math.min(startX, point.x);
  draftImage.y = Math.min(startY, point.y);
  draftImage.width = Math.abs(point.x - startX);
  draftImage.height = Math.abs(point.y - startY);
}

function finishImageSelection(event: PointerEvent) {
  if (resizeState?.pointerId === event.pointerId) {
    finishResize(event);
    return;
  }
  if (!draftImage || drawingPointer !== event.pointerId) return;
  event.preventDefault();
  event.stopPropagation();
  surfaceElement.value?.releasePointerCapture(event.pointerId);
  const annotation = draftImage;
  draftImage = null;
  drawingStart = null;
  drawingPointer = -1;
  armed.value = false;
  if (annotation.width < 0.01 || annotation.height < 0.01) {
    imageAnnotations.value = imageAnnotations.value.filter((item) => item.id !== annotation.id);
    return;
  }
  selectedId.value = annotation.id;
  openImageEditor(annotation, false);
}

function cancelImageSelection(event: PointerEvent) {
  if (resizeState?.pointerId === event.pointerId) {
    finishResize(event);
    return;
  }
  if (!draftImage || drawingPointer !== event.pointerId) return;
  imageAnnotations.value = imageAnnotations.value.filter((item) => item.id !== draftImage?.id);
  draftImage = null;
  drawingStart = null;
  drawingPointer = -1;
  armed.value = false;
}

function normalizedImagePoint(event: PointerEvent, clamp = false) {
  const bounds = imageBounds.value;
  const surface = surfaceElement.value;
  if (!surface || bounds.width <= 0 || bounds.height <= 0) return null;
  const surfaceRect = surface.getBoundingClientRect();
  let x = (event.clientX - surfaceRect.left - bounds.left) / bounds.width;
  let y = (event.clientY - surfaceRect.top - bounds.top) / bounds.height;
  if (!clamp && (x < 0 || x > 1 || y < 0 || y > 1)) return null;
  x = Math.max(0, Math.min(1, x));
  y = Math.max(0, Math.min(1, y));
  return { x, y };
}

function imageAnnotationStyle(annotation: ImagePreviewAnnotation) {
  const bounds = imageBounds.value;
  return {
    left: `${bounds.left + annotation.x * bounds.width}px`,
    top: `${bounds.top + annotation.y * bounds.height}px`,
    width: `${annotation.width * bounds.width}px`,
    height: `${annotation.height * bounds.height}px`,
  };
}

function selectAnnotation(id: string) {
  if (isDisplayAnnotation(id)) return;
  selectedId.value = id;
}

function isDisplayAnnotation(id: string) {
  return displayAnnotationIDs.value.has(id);
}

function editImageAnnotation(annotation: ImagePreviewAnnotation) {
  if (!isDisplayAnnotation(annotation.id)) openImageEditor(annotation);
}

function editTextAnnotation(annotation: StoredTextAnnotation) {
  if (!isDisplayAnnotation(annotation.id)) openTextAnnotationEditor(annotation);
}

function startResize(
  event: PointerEvent,
  annotation: ImagePreviewAnnotation,
  corner: ResizeCorner,
) {
  selectedId.value = annotation.id;
  resizeState = {
    pointerId: event.pointerId,
    annotation,
    corner,
    startX: event.clientX,
    startY: event.clientY,
    original: { ...annotation },
  };
  surfaceElement.value?.setPointerCapture(event.pointerId);
  event.stopPropagation();
}

function resizeAnnotation(event: PointerEvent) {
  const state = resizeState;
  const bounds = imageBounds.value;
  if (!state || bounds.width <= 0 || bounds.height <= 0) return;
  event.preventDefault();
  event.stopPropagation();
  const dx = (event.clientX - state.startX) / bounds.width;
  const dy = (event.clientY - state.startY) / bounds.height;
  const right = state.original.x + state.original.width;
  const bottom = state.original.y + state.original.height;
  const minSize = 0.01;
  if (state.corner.includes('w')) {
    state.annotation.x = Math.max(0, Math.min(right - minSize, state.original.x + dx));
    state.annotation.width = right - state.annotation.x;
  } else {
    state.annotation.width = Math.max(
      minSize,
      Math.min(1 - state.original.x, state.original.width + dx),
    );
  }
  if (state.corner.includes('n')) {
    state.annotation.y = Math.max(0, Math.min(bottom - minSize, state.original.y + dy));
    state.annotation.height = bottom - state.annotation.y;
  } else {
    state.annotation.height = Math.max(
      minSize,
      Math.min(1 - state.original.y, state.original.height + dy),
    );
  }
}

function finishResize(event: PointerEvent) {
  if (!resizeState || resizeState.pointerId !== event.pointerId) return;
  event.preventDefault();
  event.stopPropagation();
  surfaceElement.value?.releasePointerCapture(event.pointerId);
  resizeState = undefined;
}

function captureTextSelection() {
  if (props.mode !== 'text') return;
  const surface = surfaceElement.value;
  const selection = window.getSelection();
  if (!surface || !selection || selection.rangeCount === 0) {
    pendingTextRange.value = null;
    return;
  }
  if (selection.isCollapsed) return;
  const range = selection.getRangeAt(0);
  const container =
    range.commonAncestorContainer.nodeType === Node.ELEMENT_NODE
      ? (range.commonAncestorContainer as Element)
      : range.commonAncestorContainer.parentElement;
  pendingTextRange.value = container && surface.contains(container) ? range.cloneRange() : null;
}

function openTextEditor() {
  const range = pendingTextRange.value;
  if (!range) return;
  editorKind.value = 'text';
  editorId.value = '';
  editorExisting.value = false;
  editorQuote.value = selectedAnnotationText(range);
  editorNote.value = '';
  editorOpen.value = true;
}

function selectedAnnotationText(range: Range) {
  const surface = surfaceElement.value;
  if (!surface) return '';
  return [...surface.querySelectorAll<HTMLElement>('[data-annotation-text]')]
    .filter((textRoot) => range.intersectsNode(textRoot))
    .map((textRoot) => {
      const selected = document.createRange();
      selected.selectNodeContents(textRoot);
      if (textRoot.contains(range.startContainer)) {
        selected.setStart(range.startContainer, range.startOffset);
      }
      if (textRoot.contains(range.endContainer)) {
        selected.setEnd(range.endContainer, range.endOffset);
      }
      return selected.toString();
    })
    .join('\n');
}

function openImageEditor(annotation: ImagePreviewAnnotation, existing = true) {
  editorKind.value = 'image';
  editorId.value = annotation.id;
  editorExisting.value = existing;
  editorQuote.value = '';
  editorNote.value = annotation.note;
  editorOpen.value = true;
}

function openTextAnnotationEditor(annotation: StoredTextAnnotation) {
  editorKind.value = 'text';
  editorId.value = annotation.id;
  editorExisting.value = true;
  editorQuote.value = annotation.quote;
  editorNote.value = annotation.note;
  editorOpen.value = true;
}

function saveEditor() {
  if (editorKind.value === 'image') {
    const annotation = imageAnnotations.value.find((item) => item.id === editorId.value);
    if (annotation) annotation.note = editorNote.value.trim();
  } else if (editorExisting.value) {
    const annotation = textAnnotations.value.find((item) => item.id === editorId.value);
    if (annotation) annotation.note = editorNote.value.trim();
  } else {
    const range = pendingTextRange.value;
    if (!range || !editorQuote.value.trim()) return;
    const textRange = sourceTextRange(range);
    if (!textRange) return;
    textAnnotations.value = [
      ...textAnnotations.value,
      {
        id: annotationId(),
        kind: 'text',
        start: textRange.start,
        end: textRange.end,
        quote: editorQuote.value,
        note: editorNote.value.trim(),
        range,
      },
    ];
    pendingTextRange.value = null;
    window.getSelection()?.removeAllRanges();
    void nextTick(syncTextHighlights);
  }
  closeEditor();
}

function sourceTextRange(range: Range) {
  const start = sourceTextPosition(range.startContainer, range.startOffset);
  const end = sourceTextPosition(range.endContainer, range.endOffset);
  return start && end ? { start, end } : null;
}

function sourceTextPosition(container: Node, offset: number): TextAnnotationPosition | null {
  const element =
    container.nodeType === Node.ELEMENT_NODE ? (container as Element) : container.parentElement;
  const textRoot = element?.closest<HTMLElement>('[data-annotation-text]');
  if (!textRoot) return null;
  const firstLine = Number(textRoot.dataset.annotationLine);
  if (!Number.isInteger(firstLine) || firstLine < 1) return null;
  const prefix = document.createRange();
  prefix.selectNodeContents(textRoot);
  prefix.setEnd(container, offset);
  const lines = prefix.toString().split('\n');
  const revision = textRoot.dataset.annotationRevision;
  return {
    line: firstLine + lines.length - 1,
    column: (lines.at(-1)?.length ?? 0) + 1,
    ...(revision === 'old' || revision === 'new' ? { revision } : {}),
  };
}

function cancelEditor() {
  if (editorKind.value === 'image' && !editorExisting.value && editorId.value) {
    imageAnnotations.value = imageAnnotations.value.filter((item) => item.id !== editorId.value);
    selectedId.value = '';
  }
  closeEditor();
}

function deleteEditorAnnotation() {
  if (editorKind.value === 'image') {
    imageAnnotations.value = imageAnnotations.value.filter((item) => item.id !== editorId.value);
  } else {
    textAnnotations.value = textAnnotations.value.filter((item) => item.id !== editorId.value);
    syncTextHighlights();
  }
  selectedId.value = '';
  closeEditor();
}

function closeEditor() {
  editorOpen.value = false;
  editorNote.value = '';
  editorQuote.value = '';
  editorId.value = '';
  editorExisting.value = false;
}

function syncImageBounds() {
  const surface = surfaceElement.value;
  const image = surface?.querySelector('img');
  if (!surface || !image) return;
  const surfaceRect = surface.getBoundingClientRect();
  const imageRect = image.getBoundingClientRect();
  imageBounds.value = {
    left: imageRect.left - surfaceRect.left,
    top: imageRect.top - surfaceRect.top,
    width: imageRect.width,
    height: imageRect.height,
  };
}

function syncTextHighlights() {
  const surface = surfaceElement.value;
  if (!surface) return;
  const surfaceRect = surface.getBoundingClientRect();
  const visibleAnnotations = [...textAnnotations.value, ...displayTextAnnotations.value];
  textHighlights.value = visibleAnnotations.flatMap((annotation, annotationIndex) =>
    [...annotation.range.getClientRects()].map((rect, index) => ({
      key: `${annotation.id}:${index}`,
      annotation,
      index: annotationIndex + 1,
      style: {
        left: `${rect.left - surfaceRect.left}px`,
        top: `${rect.top - surfaceRect.top}px`,
        width: `${rect.width}px`,
        height: `${rect.height}px`,
      },
    })),
  );
}

function syncDisplayTextAnnotations() {
  if (props.mode !== 'text') return;
  displayTextAnnotations.value = props.displayAnnotations.flatMap((annotation) => {
    if (annotation.kind !== 'text') return [];
    const range = annotationRange(annotation);
    return range ? [{ ...annotation, range }] : [];
  });
  syncTextHighlights();
}

function annotationRange(annotation: TextPreviewAnnotation) {
  const start = annotationTextPoint(annotation.start);
  const end = annotationTextPoint(annotation.end);
  if (!start || !end) return null;
  const range = document.createRange();
  try {
    range.setStart(start.node, start.offset);
    range.setEnd(end.node, end.offset);
    return range;
  } catch {
    return null;
  }
}

function annotationTextPoint(position: TextAnnotationPosition) {
  const surface = surfaceElement.value;
  if (!surface) return null;
  for (const root of surface.querySelectorAll<HTMLElement>('[data-annotation-text]')) {
    if (position.revision && root.dataset.annotationRevision !== position.revision) continue;
    const firstLine = Number(root.dataset.annotationLine);
    const lines = (root.textContent ?? '').split('\n');
    const lineOffset = position.line - firstLine;
    if (!Number.isInteger(firstLine) || lineOffset < 0 || lineOffset >= lines.length) continue;
    let offset = Math.min(Math.max(position.column - 1, 0), lines[lineOffset]?.length ?? 0);
    for (let index = 0; index < lineOffset; index++) offset += (lines[index]?.length ?? 0) + 1;
    const point = textNodePoint(root, offset);
    if (point) return point;
  }
  return null;
}

function textNodePoint(root: HTMLElement, offset: number) {
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  let node = walker.nextNode();
  let remaining = offset;
  let last: Text | null = null;
  while (node) {
    const textNode = node as Text;
    last = textNode;
    if (remaining <= textNode.data.length) return { node: textNode, offset: remaining };
    remaining -= textNode.data.length;
    node = walker.nextNode();
  }
  return last ? { node: last, offset: last.data.length } : null;
}

function syncOverlay() {
  if (props.mode === 'image') syncImageBounds();
  else syncDisplayTextAnnotations();
}

function injectAnnotations() {
  if (!injector || annotations.value.length === 0 || !canInject.value) return;
  const fileReferences = annotationFileReferences();
  injector.inject(
    {
      id: `${Date.now()}-${Math.random().toString(36).slice(2)}`,
      source: props.source.trim() || '当前内容',
      content: formatPreviewAnnotationDraft(props.source, annotations.value),
      marks: annotations.value,
      ...(fileReferences.length > 0 ? { fileReferences } : {}),
    },
    props.sessionId,
  );
  clearAnnotations();
}

function annotationFileReferences() {
  if (props.mode !== 'text') return props.fileReferences;
  const revisions = new Set(
    textAnnotations.value.flatMap((annotation) =>
      [annotation.start.revision, annotation.end.revision].filter(
        (revision): revision is 'old' | 'new' => Boolean(revision),
      ),
    ),
  );
  return props.fileReferences.filter(
    (reference) =>
      reference.kind !== 'diff' || !reference.version || revisions.has(reference.version),
  );
}

function clearAnnotations() {
  imageAnnotations.value = [];
  textAnnotations.value = [];
  textHighlights.value = [];
  pendingTextRange.value = null;
  selectedId.value = '';
  armed.value = false;
  draftImage = null;
  drawingStart = null;
  drawingPointer = -1;
  resizeState = undefined;
  window.getSelection()?.removeAllRanges();
}

function cornerLabel(corner: ResizeCorner) {
  const labels: Record<ResizeCorner, string> = {
    nw: '左上角',
    ne: '右上角',
    sw: '左下角',
    se: '右下角',
  };
  return labels[corner];
}

watch(
  () => props.contentKey,
  () => {
    clearAnnotations();
    void nextTick(syncOverlay);
  },
);
watch(
  () => props.displayAnnotations,
  () => void nextTick(syncOverlay),
  { deep: true },
);

onMounted(() => {
  const surface = surfaceElement.value;
  if (!surface) return;
  syncOverlay();
  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(syncOverlay);
    resizeObserver.observe(surface);
    const image = surface.querySelector('img');
    if (image) resizeObserver.observe(image);
  }
  const image = surface.querySelector('img');
  if (typeof MutationObserver !== 'undefined') {
    mutationObserver = new MutationObserver((mutations) => {
      const textChanged = mutations.some((mutation) => {
        const element =
          mutation.target.nodeType === Node.ELEMENT_NODE
            ? (mutation.target as Element)
            : mutation.target.parentElement;
        return Boolean(
          element?.closest('[data-annotation-text]') ||
          [...mutation.addedNodes].some(
            (node) =>
              node.nodeType === Node.ELEMENT_NODE &&
              ((node as Element).matches('[data-annotation-text]') ||
                Boolean((node as Element).querySelector('[data-annotation-text]'))),
          ),
        );
      });
      if (image || textChanged) syncOverlay();
    });
    if (image) mutationObserver.observe(image, { attributes: true, attributeFilter: ['style'] });
    else mutationObserver.observe(surface, { childList: true, characterData: true, subtree: true });
  }
  surface.addEventListener('scroll', syncOverlay, true);
  document.addEventListener('selectionchange', captureTextSelection);
  window.addEventListener('resize', syncOverlay);
});

onBeforeUnmount(() => {
  const surface = surfaceElement.value;
  surface?.removeEventListener('scroll', syncOverlay, true);
  document.removeEventListener('selectionchange', captureTextSelection);
  window.removeEventListener('resize', syncOverlay);
  resizeObserver?.disconnect();
  mutationObserver?.disconnect();
});
</script>

<style scoped>
.preview-annotator {
  display: flex;
  width: 100%;
  min-width: 0;
  min-height: 0;
  flex-direction: column;
}

.preview-annotator__toolbar {
  position: sticky;
  z-index: 4;
  top: 0;
  display: flex;
  min-width: 0;
  min-height: 40px;
  align-items: center;
  gap: 8px;
  padding: 4px 8px;
  color: var(--ac-text);
  background: color-mix(in srgb, var(--ac-surface-raised) 94%, transparent);
  border-bottom: 1px solid var(--ac-border);
  backdrop-filter: blur(8px);
}

.preview-annotator__toolbar-leading,
.preview-annotator__toolbar-controls,
.preview-annotator__toolbar-actions {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 8px;
}

.preview-annotator__toolbar-leading {
  max-width: min(32%, 320px);
  flex: 0 1 auto;
}

.preview-annotator__toolbar-leading:empty,
.preview-annotator__toolbar-actions:empty {
  display: none;
}

.preview-annotator__toolbar-controls {
  flex: 1 1 auto;
}

.preview-annotator__toolbar-actions {
  flex: 0 0 auto;
}

.preview-annotator__surface {
  position: relative;
  width: 100%;
  min-width: 0;
  min-height: 0;
  flex: 1 1 auto;
}

.preview-annotator--image .preview-annotator__surface {
  display: grid;
  place-items: center;
}

.preview-annotator__surface--armed {
  cursor: crosshair;
  touch-action: none;
  user-select: none;
}

.preview-annotator__shape {
  position: absolute;
  z-index: 2;
  box-sizing: border-box;
  min-width: 8px;
  min-height: 8px;
  background: color-mix(in srgb, var(--q-primary) 12%, transparent);
  border: 2px solid var(--q-primary);
  cursor: pointer;
}

.preview-annotator__shape--ellipse {
  border-radius: 50%;
}

.preview-annotator__shape--selected {
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--q-primary) 28%, transparent);
}

.preview-annotator__marker-index {
  position: absolute;
  top: 2px;
  left: 2px;
  display: grid;
  min-width: 18px;
  height: 18px;
  padding: 0 4px;
  place-items: center;
  color: var(--ac-on-primary-container);
  font-size: 11px;
  font-weight: 700;
  line-height: 1;
  pointer-events: none;
  background: var(--ac-primary-container);
  border-radius: 999px;
}

.preview-annotator__highlight .preview-annotator__marker-index {
  top: -18px;
  left: 0;
}

.preview-annotator__shape-note {
  position: absolute;
  top: 4px;
  right: 4px;
  color: var(--ac-on-primary-container);
  background: var(--ac-primary-container);
}

.preview-annotator__resize-handle {
  position: absolute;
  width: 12px;
  height: 12px;
  padding: 0;
  background: var(--ac-surface);
  border: 2px solid var(--q-primary);
  border-radius: 50%;
}

.preview-annotator__resize-handle--nw {
  top: -7px;
  left: -7px;
  cursor: nwse-resize;
}

.preview-annotator__resize-handle--ne {
  top: -7px;
  right: -7px;
  cursor: nesw-resize;
}

.preview-annotator__resize-handle--sw {
  bottom: -7px;
  left: -7px;
  cursor: nesw-resize;
}

.preview-annotator__resize-handle--se {
  right: -7px;
  bottom: -7px;
  cursor: nwse-resize;
}

.preview-annotator__highlight {
  position: absolute;
  z-index: 2;
  padding: 0;
  background: color-mix(in srgb, var(--q-warning) 34%, transparent);
  border: 0;
  border-bottom: 2px solid var(--q-warning);
  cursor: pointer;
}

.preview-annotator__highlight:focus-visible,
.preview-annotator__highlight:hover {
  background: color-mix(in srgb, var(--q-warning) 48%, transparent);
  outline: 2px solid var(--q-warning);
}

.preview-annotator__editor {
  width: min(520px, calc(100vw - 24px));
}

.preview-annotator__quote {
  max-height: 180px;
  overflow: auto;
  color: var(--ac-text-muted);
  white-space: pre-wrap;
  background: var(--ac-surface-muted);
  border-left: 3px solid var(--q-primary);
}

@media (max-width: 599.98px) {
  .preview-annotator__toolbar {
    padding: max(4px, env(safe-area-inset-top)) max(8px, env(safe-area-inset-right)) 4px
      max(8px, env(safe-area-inset-left));
  }

  .preview-annotator__toolbar-leading {
    display: none;
  }

  .preview-annotator__toolbar-controls {
    overflow-x: auto;
    overscroll-behavior-x: contain;
  }

  .preview-annotator__toolbar :deep(.q-btn__content .block) {
    white-space: nowrap;
  }
}
</style>
