import { defineBoot } from '#q-app';

import { getAppearanceSettings } from '@/services/appearanceSettings';
import { getGraphQLAccessKey } from '@/services/graphqlClient';
import { applyAppearanceSettings } from '@/theme/appearance';
import { initializeDailyBackground } from '@/theme/dailyBackground';
import { applyThemeMode, readThemeMode } from '@/theme/tokens';

export default defineBoot(() => {
  applyThemeMode(readThemeMode());
  initializeDailyBackground();
  if (getGraphQLAccessKey()) {
    void getAppearanceSettings({ notify: false })
      .then(applyAppearanceSettings)
      .catch(() => undefined);
  }
});
