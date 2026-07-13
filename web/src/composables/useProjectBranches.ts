import { ref } from 'vue';

import { getProjectBranches, type ProjectBranchState } from '@/services/projects';

const branchCache = ref<Record<string, ProjectBranchState>>({});
const branchLoading = ref<Record<string, boolean>>({});
const branchRequests = new Map<
  string,
  { promise: Promise<ProjectBranchState>; refresh: boolean }
>();

export function useProjectBranches() {
  async function loadProjectBranches(projectId: string, options: { refresh?: boolean } = {}) {
    if (!projectId) return fallbackBranches();
    const pending = branchRequests.get(projectId);
    if (pending) {
      if (!options.refresh || pending.refresh) return pending.promise;
      try {
        await pending.promise;
      } catch {
        // The explicit refresh below supersedes the earlier request result.
      }
      return loadProjectBranches(projectId, options);
    }
    if (!options.refresh && branchCache.value[projectId]) {
      return branchCache.value[projectId];
    }
    if (options.refresh) {
      const next = { ...branchCache.value };
      delete next[projectId];
      branchCache.value = next;
    }
    branchLoading.value = { ...branchLoading.value, [projectId]: true };
    const entry = {
      refresh: Boolean(options.refresh),
      promise: Promise.resolve(fallbackBranches()),
    };
    entry.promise = getProjectBranches(projectId, options)
      .then((state) => {
        branchCache.value = { ...branchCache.value, [projectId]: state };
        return state;
      })
      .finally(() => {
        if (branchRequests.get(projectId) !== entry) return;
        branchRequests.delete(projectId);
        const next = { ...branchLoading.value };
        delete next[projectId];
        branchLoading.value = next;
      });
    branchRequests.set(projectId, entry);
    return entry.promise;
  }

  return {
    branchCache,
    branchLoading,
    loadProjectBranches,
  };
}

function fallbackBranches(): ProjectBranchState {
  return { defaultBranch: '', branches: [] };
}
