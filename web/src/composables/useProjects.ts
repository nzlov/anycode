import { computed, ref } from 'vue';

import {
  cloneProject,
  createProject,
  listProjects,
  removeProject,
  updateProjectSettings,
  type ProjectSummary,
} from '@/services/projects';

const projects = ref<ProjectSummary[]>([]);
const loading = ref(false);
const loaded = ref(false);
let loadPromise: Promise<void> | null = null;
let mutationRevision = 0;

export function useProjects() {
  const projectOptions = computed(() =>
    projects.value.map((project) => ({
      label: project.name,
      value: project.id,
    })),
  );

  function loadProjects() {
    if (loadPromise) return loadPromise;
    const revision = mutationRevision;
    loading.value = true;
    loadPromise = listProjects()
      .then((result) => {
        if (revision !== mutationRevision) return;
        projects.value = result;
        loaded.value = true;
      })
      .finally(() => {
        loading.value = false;
        loadPromise = null;
      });
    return loadPromise;
  }

  async function createProjectFromPath(path: string) {
    const project = await createProject({ path, name: basename(path) });
    storeCreatedProject(project);
    return project;
  }

  async function createRemoteProject(parentPath: string, repositoryUrl: string) {
    const project = await cloneProject({ parentPath, repositoryUrl });
    storeCreatedProject(project);
    return project;
  }

  function storeCreatedProject(project: ProjectSummary) {
    mutationRevision += 1;
    const existingIndex = projects.value.findIndex((item) => item.id === project.id);
    if (existingIndex >= 0) {
      projects.value.splice(existingIndex, 1, project);
    } else {
      projects.value.push(project);
    }
  }

  async function removeProjectById(id: string) {
    await removeProject(id);
    mutationRevision += 1;
    projects.value = projects.value.filter((project) => project.id !== id);
  }

  async function updateProjectSettingsById(input: {
    projectId: string;
    worktreeInitCommand: string;
    mindMapEnabled: boolean;
  }) {
    const project = await updateProjectSettings(input);
    mutationRevision += 1;
    const index = projects.value.findIndex((item) => item.id === project.id);
    if (index >= 0) {
      const current = projects.value[index]!;
      projects.value.splice(index, 1, {
        ...project,
        active: current.active,
        openSessions: current.openSessions,
      });
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
    createRemoteProject,
    removeProjectById,
    updateProjectSettingsById,
  };
}

function basename(path: string) {
  return path.split('/').filter(Boolean).at(-1) ?? path;
}
