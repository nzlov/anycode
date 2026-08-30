import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

function read(relativePath) {
  return readFileSync(new URL(`../${relativePath}`, import.meta.url), 'utf8');
}

test('transcript pages return byte references and expose an explicit event loader', () => {
  const source = read('src/services/sessionTimeline.ts');
  assert.match(source, /deferred \{ byteOffset byteLength \}/);
  assert.match(source, /query SessionTranscriptEvent\(\$input: SessionTranscriptEventInput!\)/);
  const loader = read('src/services/sessionTranscriptContent.ts');
  assert.match(
    loader,
    /event\.deferredEventId \?\? event\.id/,
  );
});

test('large transcript bodies load only from expandable event controls', () => {
  for (const component of [
    'SessionCommandEvent.vue',
    'SessionToolEvent.vue',
    'SessionReasoningEvent.vue',
    'SessionFileChangeEvent.vue',
    'SessionStatusEvent.vue',
    'SessionUnknownEvent.vue',
  ]) {
    const source = read(`src/components/${component}`);
    assert.match(source, /useDeferredTranscriptEvent/);
    assert.match(
      source,
      component === 'SessionToolEvent.vue'
        ? /if \(!expanded\.value\) return;[\s\S]*await load\(\)/
        : /if \(expanded\.value\) void load\(\)/,
    );
  }
  const message = read('src/components/SessionTextMessage.vue');
  assert.match(message, /event\.deferred && resolvedEvent\.deferred/);
  assert.match(message, /@click="load"/);

  const composable = read('src/composables/useDeferredTranscriptEvent.ts');
  assert.match(composable, /\{ \.\.\.result, id: current\.id, sourceEventIds: current\.sourceEventIds \}/);
});
