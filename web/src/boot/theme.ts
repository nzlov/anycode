import { defineBoot } from '#q-app';

import { applyThemeMode, readThemeMode } from '@/theme/tokens';

export default defineBoot(() => {
  applyThemeMode(readThemeMode());
});
