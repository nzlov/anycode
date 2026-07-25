<template>
  <div v-if="thinkingPhrasesEnabled" class="session-thinking-phrase">{{ phrase }}</div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue';

import { useSessionThinkingPhrases } from '@/composables/useSessionThinkingPhrases';

const props = defineProps<{
  refreshKey?: unknown;
}>();

const { thinkingPhrasesEnabled, thinkingPhraseStyle, thinkingPhrases } =
  useSessionThinkingPhrases();
const phrase = ref('');
let phraseTimer: ReturnType<typeof setInterval> | undefined;

function changePhrase() {
  const candidates = thinkingPhrases.value.filter((candidate) => candidate !== phrase.value);
  phrase.value =
    candidates[Math.floor(Math.random() * candidates.length)] ?? thinkingPhrases.value[0] ?? '';
}

watch([() => props.refreshKey, thinkingPhraseStyle], () => {
  if (thinkingPhrasesEnabled.value) changePhrase();
});
watch(thinkingPhrasesEnabled, (enabled) => {
  if (!enabled) {
    stopPhraseTimer();
    phrase.value = '';
    return;
  }
  changePhrase();
  startPhraseTimer();
});

onMounted(() => {
  if (!thinkingPhrasesEnabled.value) return;
  changePhrase();
  startPhraseTimer();
});

onUnmounted(stopPhraseTimer);

function startPhraseTimer() {
  if (!phraseTimer) phraseTimer = setInterval(changePhrase, 5000);
}

function stopPhraseTimer() {
  if (!phraseTimer) return;
  clearInterval(phraseTimer);
  phraseTimer = undefined;
}
</script>

<style scoped>
.session-thinking-phrase {
  min-width: 0;
  overflow: hidden;
  color: var(--ac-text-muted);
  font-size: 12px;
  font-style: italic;
  line-height: 1.5;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
