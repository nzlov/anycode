import { defineBoot } from '#q-app';

import { getAppearanceSettings } from '@/services/appearanceSettings';
import { getGraphQLAccessKey } from '@/services/graphqlClient';
import { initializeDailyBackground, setWallpaperColorScheme } from '@/theme/dailyBackground';
import { applyThemeMode, readThemeMode } from '@/theme/tokens';

export default defineBoot(() => {
  applyThemeMode(readThemeMode());
  initializeDailyBackground();
  if (getGraphQLAccessKey()) {
    void getAppearanceSettings({ notify: false })
      .then(({ wallpaperColorScheme }) => setWallpaperColorScheme(wallpaperColorScheme))
      .catch(() => undefined);
  }
});
