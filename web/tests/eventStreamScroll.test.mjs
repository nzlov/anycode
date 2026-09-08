import assert from 'node:assert/strict';
import test from 'node:test';
import { nextTick, shallowRef } from 'vue';

import { useEventStreamScroll } from '../src/composables/useEventStreamScroll.ts';

function scrollBody() {
  let top = 0;
  return {
    scrollHeight: 1000,
    clientHeight: 200,
    get scrollTop() {
      return top;
    },
    set scrollTop(value) {
      top = Math.max(0, Math.min(value, this.scrollHeight - this.clientHeight));
    },
  };
}

test('new events follow the bottom, preserve history reading, and resume after returning to bottom', async () => {
  const body = scrollBody();
  const scroll = useEventStreamScroll(shallowRef(body));
  await scroll.scrollEventsToBottom(true);
  assert.equal(body.scrollTop, 800);
  body.scrollHeight = 1200;
  scroll.followLatestEvent();
  await nextTick();
  assert.equal(body.scrollTop, 1000);
  body.scrollTop = 450;
  assert.equal(scroll.updateEventScroll(), true);
  body.scrollHeight = 1500;
  scroll.followLatestEvent();
  await nextTick();
  assert.equal(body.scrollTop, 450);
  body.scrollTop = 1300;
  scroll.updateEventScroll();
  body.scrollHeight = 1700;
  scroll.followLatestEvent();
  await nextTick();
  assert.equal(body.scrollTop, 1500);
});

test('an upward scroll before the DOM update cancels queued following', async () => {
  const body = scrollBody();
  const scroll = useEventStreamScroll(shallowRef(body));
  await scroll.scrollEventsToBottom(true);
  scroll.followLatestEvent();
  body.scrollTop = 700;
  scroll.updateEventScroll();
  await nextTick();
  assert.equal(body.scrollTop, 700);
});

test('loading older pages preserves following state and reopening a Side starts at the latest event', async () => {
  const body = scrollBody();
  let paused = false;
  const bodyRef = shallowRef(body);
  const scroll = useEventStreamScroll(bodyRef, () => paused);
  await scroll.scrollEventsToBottom(true);
  paused = true;
  body.scrollHeight = 1800;
  scroll.updateEventScroll();
  scroll.followLatestEvent();
  await nextTick();
  assert.equal(body.scrollTop, 800);
  paused = false;
  scroll.followLatestEvent();
  await nextTick();
  assert.equal(body.scrollTop, 1600);
  body.scrollTop = 400;
  scroll.updateEventScroll();
  bodyRef.value = scrollBody();
  await scroll.scrollEventsToBottom(true);
  assert.equal(bodyRef.value.scrollTop, 800);
  bodyRef.value = null;
  await scroll.scrollEventsToBottom();
});
