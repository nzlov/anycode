<template>
  <q-pagination
    :model-value="modelValue"
    dense
    class="app-pagination"
    :boundary-numbers="!$q.screen.lt.sm"
    direction-links
    size="24px"
    :max="max"
    :max-pages="$q.screen.lt.sm ? 3 : 5"
    :disable="disabled"
    @update:model-value="emit('update:modelValue', $event)"
  />
</template>

<script setup lang="ts">
import { useQuasar } from 'quasar';

withDefaults(
  defineProps<{
    modelValue: number;
    max: number;
    disabled?: boolean;
  }>(),
  { disabled: false },
);

const emit = defineEmits<{
  'update:modelValue': [value: number];
}>();

const $q = useQuasar();
</script>
