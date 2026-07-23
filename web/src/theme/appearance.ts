import type { AppearanceSettings } from '@/services/appearanceSettings';
import {
  activateBingBackground,
  activateSolidBackground,
  activateUploadedBackground,
  setBackgroundMask,
} from '@/theme/dailyBackground';
import { solidThemeOptions } from '@/theme/solidThemes';

export function applyAppearanceSettings(settings: AppearanceSettings) {
  setBackgroundMask(settings.backgroundMask);
  if (settings.backgroundType === 'solid') {
    const theme = solidThemeOptions.find((option) => option.value === settings.solidTheme);
    if (theme) activateSolidBackground(theme.color);
    return;
  }
  if (settings.backgroundType === 'image') {
    void activateUploadedBackground(settings.wallpaperId, settings.wallpaperColorScheme);
    return;
  }
  activateBingBackground(settings.wallpaperColorScheme);
}
