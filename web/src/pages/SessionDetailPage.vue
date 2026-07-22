<template>
  <SessionDetailView
    :session-id="sessionId"
    layout="responsive"
    page
    @session-title="emit('session-title', $event)"
  />
</template>

<script setup lang="ts">
import { computed } from 'vue';
import { useRoute } from 'vue-router';

import SessionDetailView from '@/components/SessionDetailView.vue';

const emit = defineEmits<{
  'session-title': [title: string];
}>();
const route = useRoute();
const sessionId = computed(() => String(route.params.id ?? ''));
</script>
