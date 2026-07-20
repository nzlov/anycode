import { defineBoot } from '#q-app';

import { initializeDailyBackground } from '@/theme/dailyBackground';
import { applyThemeMode, readThemeMode } from '@/theme/tokens';

export default defineBoot(() => {
  applyThemeMode(readThemeMode());
  initializeDailyBackground();
});
