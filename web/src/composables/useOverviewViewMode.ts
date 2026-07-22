import { computed, ref } from 'vue';

export type OverviewViewMode = 'card' | 'horizontal';

export const overviewViewModeStorageKey = 'anycode.overview.view-mode.v1';

const storedOverviewViewMode = ref<OverviewViewMode>('card');
let initialized = false;

export function useOverviewViewMode() {
  if (!initialized) {
    storedOverviewViewMode.value = readOverviewViewMode();
    initialized = true;
  }

  const overviewViewMode = computed({
    get: () => storedOverviewViewMode.value,
    set: (mode: OverviewViewMode) => {
      storedOverviewViewMode.value = mode;
      writeOverviewViewMode(mode);
    },
  });

  return { overviewViewMode };
}

export function readOverviewViewMode(): OverviewViewMode {
  if (typeof window === 'undefined') return 'card';
  try {
    return window.localStorage.getItem(overviewViewModeStorageKey) === 'horizontal'
      ? 'horizontal'
      : 'card';
  } catch {
    return 'card';
  }
}

function writeOverviewViewMode(mode: OverviewViewMode) {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(overviewViewModeStorageKey, mode);
  } catch {
    // The selected mode remains active when browser storage is unavailable.
  }
}
