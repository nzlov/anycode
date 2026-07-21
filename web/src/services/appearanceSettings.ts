import { graphqlFetch } from '@/services/graphqlClient';
import { isWallpaperColorScheme, type WallpaperColorScheme } from '@/theme/dailyBackgroundModel';

export interface AppearanceSettings {
  wallpaperColorScheme: WallpaperColorScheme;
}

export const wallpaperColorSchemeOptions: Array<{
  label: string;
  value: WallpaperColorScheme;
}> = [
  { label: 'M3 Content', value: 'content' },
  { label: 'M3 Fidelity', value: 'fidelity' },
  { label: 'M3 Tonal Spot', value: 'tonal_spot' },
  { label: 'M3 Vibrant', value: 'vibrant' },
  { label: 'M3 Expressive', value: 'expressive' },
  { label: 'M3 Rainbow', value: 'rainbow' },
  { label: 'M3 Fruit Salad', value: 'fruit_salad' },
  { label: 'M3 Neutral', value: 'neutral' },
  { label: 'M3 Monochrome', value: 'monochrome' },
];

export async function getAppearanceSettings({ notify = true } = {}) {
  const data = await graphqlFetch<{
    appearanceSettings: { wallpaperColorScheme: string };
  }>({
    query: `
      query AppearanceSettings {
        appearanceSettings {
          wallpaperColorScheme
        }
      }
    `,
    notify,
  });
  return normalizeAppearanceSettings(data.appearanceSettings);
}

export async function updateAppearanceSettings(wallpaperColorScheme: WallpaperColorScheme) {
  const data = await graphqlFetch<
    { updateAppearanceSettings: { wallpaperColorScheme: string } },
    { input: { wallpaperColorScheme: string } }
  >({
    query: `
      mutation UpdateAppearanceSettings($input: UpdateAppearanceSettingsInput!) {
        updateAppearanceSettings(input: $input) {
          wallpaperColorScheme
        }
      }
    `,
    variables: {
      input: { wallpaperColorScheme: wallpaperColorScheme.toUpperCase() },
    },
  });
  return normalizeAppearanceSettings(data.updateAppearanceSettings);
}

function normalizeAppearanceSettings(settings: {
  wallpaperColorScheme: string;
}): AppearanceSettings {
  const scheme = settings.wallpaperColorScheme.toLowerCase();
  return {
    wallpaperColorScheme: isWallpaperColorScheme(scheme) ? scheme : 'content',
  };
}
