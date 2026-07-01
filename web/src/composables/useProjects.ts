import { computed, ref } from 'vue';

import { createProject, listProjects, type ProjectSummary } from '@/services/projects';

const projects = ref<ProjectSummary[]>([]);
const loading = ref(false);
const loaded = ref(false);

export function useProjects() {
  const projectOptions = computed(() =>
    projects.value.map((project) => ({
      label: project.name,
      value: project.id,
    })),
  );

  async function loadProjects() {
    if (loading.value) return;
    loading.value = true;
    try {
      projects.value = await listProjects();
      loaded.value = true;
    } finally {
      loading.value = false;
    }
  }

  async function createProjectFromPath(path: string) {
    const project = await createProject({ path, name: basename(path) });
    const existingIndex = projects.value.findIndex((item) => item.id === project.id);
    if (existingIndex >= 0) {
      projects.value.splice(existingIndex, 1, project);
    } else {
      projects.value.push(project);
    }
    return project;
  }

  return {
    projects,
    projectOptions,
    loading,
    loaded,
    loadProjects,
    createProjectFromPath,
  };
}

function basename(path: string) {
  return path.split('/').filter(Boolean).at(-1) ?? path;
}
