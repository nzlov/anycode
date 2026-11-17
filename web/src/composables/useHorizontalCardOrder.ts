import { computed, nextTick, onUnmounted, ref, watch, type ComputedRef } from 'vue';

import type { SessionCard } from '@/services/sessions';

const storageKey = 'anycode.overview.horizontal-order.v1';

export function useHorizontalCardOrder(
  cards: ComputedRef<SessionCard[]>,
  enabled: ComputedRef<boolean>,
) {
  const sectionRef = ref<HTMLElement | null>(null);
  const order = ref<string[]>(readOrder());
  const draggedId = ref('');
  const dropTarget = ref<{ id: string; side: 'before' | 'after' } | null>(null);
  const announcement = ref('');
  let pointer: {
    id: number;
    cardId: string;
    handle: HTMLElement;
    startX: number;
    startY: number;
    x: number;
    y: number;
  } | null = null;
  let scrollFrame = 0;

  const orderedCards = computed(() => {
    const positions = new Map(order.value.map((id, index) => [id, index]));
    // New cards keep their creation order at the front; saved cards retain their positions.
    return [...cards.value].sort(
      (left, right) => (positions.get(left.id) ?? -1) - (positions.get(right.id) ?? -1),
    );
  });

  function readOrder(): string[] {
    try {
      const stored: unknown = JSON.parse(window.localStorage.getItem(storageKey) ?? '[]');
      return Array.isArray(stored)
        ? [...new Set(stored.filter((id): id is string => typeof id === 'string' && id !== ''))]
        : [];
    } catch {
      return [];
    }
  }

  function moveCard(cardId: string, targetId: string, side: 'before' | 'after') {
    const ids = orderedCards.value.map((card) => card.id);
    if (cardId === targetId || !ids.includes(cardId) || !ids.includes(targetId)) return;
    const next = ids.filter((id) => id !== cardId);
    next.splice(next.indexOf(targetId) + (side === 'after' ? 1 : 0), 0, cardId);
    if (next.every((id, index) => id === ids[index])) return;
    // Replace only loaded cards' slots so scoped routes preserve other projects' positions.
    const loadedIds = new Set(ids);
    const combined = [...ids.filter((id) => !order.value.includes(id)), ...order.value];
    let nextIndex = 0;
    order.value = combined.map((id) => (loadedIds.has(id) ? next[nextIndex++]! : id));
    try {
      window.localStorage.setItem(storageKey, JSON.stringify(order.value));
    } catch {
      // Reordering remains available for this page when browser storage is unavailable.
    }
    const title = cards.value.find((card) => card.id === cardId)?.title ?? '';
    announcement.value = `已调整 ${title} 的卡片位置`;
  }

  function cardElements() {
    return [...(sectionRef.value?.querySelectorAll<HTMLElement>('[data-session-id]') ?? [])];
  }

  function beginDrag(event: PointerEvent) {
    if (!enabled.value || pointer || event.button !== 0 || !event.isPrimary) return;
    const handle = (event.target as HTMLElement).closest<HTMLElement>('[data-card-drag-handle]');
    const cardId = handle?.closest<HTMLElement>('[data-session-id]')?.dataset.sessionId;
    if (!handle || !cardId || !sectionRef.value?.contains(handle)) return;
    handle.focus({ preventScroll: true });
    pointer = {
      id: event.pointerId,
      cardId,
      handle,
      startX: event.clientX,
      startY: event.clientY,
      x: event.clientX,
      y: event.clientY,
    };
    sectionRef.value.setPointerCapture(event.pointerId);
    window.addEventListener('blur', cancelDrag);
    event.preventDefault();
  }

  function updateDropTarget() {
    const section = sectionRef.value;
    if (!pointer || !section) return;
    const bounds = section.getBoundingClientRect();
    if (pointer.y < bounds.top || pointer.y > bounds.bottom) {
      dropTarget.value = null;
      return;
    }
    const elements = cardElements();
    const target =
      elements.find((element) => pointer!.x < element.getBoundingClientRect().right) ??
      elements.at(-1);
    const id = target?.dataset.sessionId;
    if (!target || !id || id === pointer.cardId) {
      dropTarget.value = null;
      return;
    }
    const rect = target.getBoundingClientRect();
    dropTarget.value = { id, side: pointer.x < rect.left + rect.width / 2 ? 'before' : 'after' };
  }

  function autoScroll() {
    const section = sectionRef.value;
    if (!pointer || !draggedId.value || !section) return;
    const bounds = section.getBoundingClientRect();
    if (pointer.y >= bounds.top && pointer.y <= bounds.bottom) {
      const edge = Math.min(64, bounds.width / 4);
      const delta =
        pointer.x < bounds.left + edge
          ? -Math.min(18, (bounds.left + edge - pointer.x) / 4)
          : pointer.x > bounds.right - edge
            ? Math.min(18, (pointer.x - bounds.right + edge) / 4)
            : 0;
      section.scrollLeft += delta;
      updateDropTarget();
    }
    scrollFrame = window.requestAnimationFrame(autoScroll);
  }

  function continueDrag(event: PointerEvent) {
    if (!pointer || pointer.id !== event.pointerId) return;
    pointer.x = event.clientX;
    pointer.y = event.clientY;
    if (
      !draggedId.value &&
      Math.hypot(pointer.x - pointer.startX, pointer.y - pointer.startY) >= 6
    ) {
      draggedId.value = pointer.cardId;
      scrollFrame = window.requestAnimationFrame(autoScroll);
    }
    if (draggedId.value) updateDropTarget();
  }

  function cancelDrag() {
    const pointerId = pointer?.id;
    pointer = null;
    draggedId.value = '';
    dropTarget.value = null;
    window.cancelAnimationFrame(scrollFrame);
    window.removeEventListener('blur', cancelDrag);
    if (pointerId !== undefined && sectionRef.value?.hasPointerCapture(pointerId)) {
      sectionRef.value.releasePointerCapture(pointerId);
    }
  }

  function finishDrag(event: PointerEvent) {
    if (!pointer || pointer.id !== event.pointerId) return;
    const handle = pointer.handle;
    pointer.x = event.clientX;
    pointer.y = event.clientY;
    if (draggedId.value) {
      updateDropTarget();
      if (dropTarget.value) moveCard(pointer.cardId, dropTarget.value.id, dropTarget.value.side);
    }
    cancelDrag();
    void nextTick(() => handle.focus({ preventScroll: true }));
  }

  function onKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape' && pointer) {
      event.preventDefault();
      event.stopPropagation();
      cancelDrag();
      return;
    }
    if (!enabled.value || pointer || !['ArrowLeft', 'ArrowRight'].includes(event.key)) return;
    const handle = (event.target as HTMLElement).closest<HTMLElement>('[data-card-drag-handle]');
    const cardId = handle?.closest<HTMLElement>('[data-session-id]')?.dataset.sessionId;
    if (!handle || !cardId) return;
    event.preventDefault();
    const elements = cardElements();
    const index = elements.findIndex((element) => element.dataset.sessionId === cardId);
    const targetId = elements[index + (event.key === 'ArrowLeft' ? -1 : 1)]?.dataset.sessionId;
    if (!targetId) return;
    moveCard(cardId, targetId, event.key === 'ArrowLeft' ? 'before' : 'after');
    void nextTick(() => {
      handle.focus({ preventScroll: true });
      handle.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    });
  }

  watch(enabled, (value) => {
    if (!value) cancelDrag();
  });
  watch(orderedCards, () => {
    if (pointer && !cards.value.some((card) => card.id === pointer?.cardId)) cancelDrag();
  });
  onUnmounted(cancelDrag);

  return {
    sectionRef,
    orderedCards,
    draggedId,
    dropTarget,
    announcement,
    beginDrag,
    continueDrag,
    finishDrag,
    cancelDrag,
    onKeydown,
  };
}
