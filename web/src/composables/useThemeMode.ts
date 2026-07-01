import { computed, ref } from 'vue';

import { readThemeMode, themeModes, writeThemeMode, type ThemeMode } from '@/theme/tokens';

const themeMode = ref<ThemeMode>('system');
let initialized = false;

export function useThemeMode() {
  if (!initialized) {
    themeMode.value = readThemeMode();
    initialized = true;
  }

  const activeThemeMode = computed({
    get: () => themeMode.value,
    set: (mode: ThemeMode) => {
      themeMode.value = mode;
      writeThemeMode(mode);
    },
  });

  return {
    themeMode: activeThemeMode,
    themeModes,
  };
}
