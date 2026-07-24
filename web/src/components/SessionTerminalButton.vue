<template>
  <q-btn
    :flat="!fullWidth"
    :round="!fullWidth"
    :dense="!fullWidth"
    :outline="fullWidth"
    :class="fullWidth ? 'full-width q-mt-md app-command-btn' : 'app-icon-btn'"
    color="primary"
    icon="terminal"
    :label="fullWidth ? '打开终端' : undefined"
    no-caps
    aria-label="打开终端"
    :loading="opening"
    @click="open"
  >
    <q-tooltip v-if="!fullWidth">打开终端</q-tooltip>
  </q-btn>
</template>

<script setup lang="ts">
import { ref } from 'vue';
import { useRouter } from 'vue-router';

import { openSessionTerminal } from '@/services/sessions';

const props = withDefaults(
  defineProps<{
    sourceSessionId: string;
    fullWidth?: boolean;
  }>(),
  { fullWidth: false },
);
const router = useRouter();
const opening = ref(false);

async function open() {
  if (opening.value) return;
  opening.value = true;
  try {
    const terminal = await openSessionTerminal(props.sourceSessionId);
    await router.push({ name: 'session-detail', params: { id: terminal.id } });
  } finally {
    opening.value = false;
  }
}
</script>
