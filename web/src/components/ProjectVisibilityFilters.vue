<template>
  <div class="overview-project-filters" role="group" aria-label="项目卡片显示筛选">
    <q-chip
      v-for="project in projects"
      :key="project.id"
      clickable
      :outline="!isProjectVisible(project.id)"
      class="overview-project-chip"
      :class="{
        'overview-project-chip--visible': isProjectVisible(project.id),
        'overview-project-chip--hidden': !isProjectVisible(project.id),
      }"
      :icon="isProjectVisible(project.id) ? 'visibility' : 'visibility_off'"
      :aria-pressed="isProjectVisible(project.id)"
      :aria-label="`${isProjectVisible(project.id) ? '隐藏' : '显示'} ${project.name} 项目卡片`"
      @click="emit('toggle', project.id)"
    >
      {{ project.name }}
    </q-chip>
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  projects: Array<{ id: string; name: string }>;
  hiddenProjectIds: ReadonlySet<string>;
}>();

const emit = defineEmits<{ toggle: [projectId: string] }>();

function isProjectVisible(projectId: string) {
  return !props.hiddenProjectIds.has(projectId);
}
</script>
