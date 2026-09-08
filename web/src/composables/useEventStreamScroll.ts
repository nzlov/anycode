import { nextTick, type Ref } from 'vue';

export function useEventStreamScroll(
  bodyRef: Ref<HTMLElement | null>,
  paused: () => boolean = () => false,
) {
  let followingLatestEvent = true;
  let previousScrollTop = Number.POSITIVE_INFINITY;

  function updateEventScroll() {
    const body = bodyRef.value;
    if (!body) return false;
    if (!paused()) {
      followingLatestEvent = body.scrollHeight - body.scrollTop - body.clientHeight <= 1;
    }
    const scrollingUp = body.scrollTop < previousScrollTop;
    previousScrollTop = body.scrollTop;
    return scrollingUp;
  }

  async function scrollEventsToBottom(force = false) {
    await nextTick();
    if (!force && (!followingLatestEvent || paused())) return;
    const body = bodyRef.value;
    if (!body) return;
    body.scrollTop = body.scrollHeight;
    previousScrollTop = body.scrollTop;
    followingLatestEvent = true;
  }

  function followLatestEvent() {
    if (!paused()) void scrollEventsToBottom();
  }

  return { updateEventScroll, scrollEventsToBottom, followLatestEvent };
}
