import { ref } from 'vue';

import { getProjectBranches, type ProjectBranchState } from '@/services/projects';

const branchCache = ref<Record<string, ProjectBranchState>>({});
const branchLoading = ref<Record<string, boolean>>({});

export function useProjectBranches() {
  async function loadProjectBranches(projectId: string, options: { refresh?: boolean } = {}) {
    if (!projectId) return fallbackBranches();
    if (!options.refresh && branchCache.value[projectId]) {
      return branchCache.value[projectId];
    }
    if (branchLoading.value[projectId]) {
      return branchCache.value[projectId] ?? fallbackBranches();
    }
    branchLoading.value = { ...branchLoading.value, [projectId]: true };
    try {
      const state = await getProjectBranches(projectId, options);
      branchCache.value = { ...branchCache.value, [projectId]: state };
      return state;
    } finally {
      const next = { ...branchLoading.value };
      delete next[projectId];
      branchLoading.value = next;
    }
  }

  return {
    branchCache,
    branchLoading,
    loadProjectBranches,
  };
}

function fallbackBranches(): ProjectBranchState {
  return { defaultBranch: 'main', branches: ['main'] };
}
