<template>
  <!-- GLUE: route pages keep action state locally; this portal only crosses the layout boundary. -->
  <Teleport defer to="#app-page-toolbar">
    <div class="page-toolbar" :class="{ 'page-toolbar--compact-title': compactTitleOnMobile }">
      <q-toolbar-title class="app-header__title page-toolbar__title">{{ title }}</q-toolbar-title>
      <div v-if="$slots.default" class="page-toolbar__actions">
        <slot />
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
withDefaults(
  defineProps<{
    title: string;
    compactTitleOnMobile?: boolean;
  }>(),
  { compactTitleOnMobile: false },
);
</script>
