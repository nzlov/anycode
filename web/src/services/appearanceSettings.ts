import { graphqlFetch, graphqlMultipartFetch } from '@/services/graphqlClient';
import { isWallpaperColorScheme, type WallpaperColorScheme } from '@/theme/dailyBackgroundModel';
import { isSolidTheme, type AppearanceSolidTheme } from '@/theme/solidThemes';

export type AppearanceBackgroundType = 'solid' | 'image' | 'bing' | 'nasa';
export type { AppearanceSolidTheme } from '@/theme/solidThemes';

export interface AppearanceSettings {
  backgroundType: AppearanceBackgroundType;
  solidTheme: AppearanceSolidTheme;
  backgroundMask: number;
  wallpaperColorScheme: WallpaperColorScheme;
  wallpaperId: string;
  wallpaperFilename: string;
}

export const backgroundTypeOptions = [
  { label: '纯色', value: 'solid' as const, icon: 'format_color_fill' },
  { label: '图片', value: 'image' as const, icon: 'image' },
  { label: 'Bing', value: 'bing' as const, icon: 'auto_awesome' },
  { label: 'NASA', value: 'nasa' as const, icon: 'rocket_launch' },
];

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

const appearanceFields = `
  backgroundType
  solidTheme
  backgroundMask
  wallpaperColorScheme
  wallpaperId
  wallpaperFilename
`;

export async function getAppearanceSettings({ notify = true } = {}) {
  const data = await graphqlFetch<{ appearanceSettings: RawAppearanceSettings }>({
    query: `
      query AppearanceSettings {
        appearanceSettings {
          ${appearanceFields}
        }
      }
    `,
    notify,
  });
  return normalizeAppearanceSettings(data.appearanceSettings);
}

export async function updateAppearanceSettings(settings: AppearanceSettings) {
  const data = await graphqlFetch<
    { updateAppearanceSettings: RawAppearanceSettings },
    {
      input: {
        backgroundType: string;
        solidTheme: string;
        backgroundMask: number;
        wallpaperColorScheme: string;
      };
    }
  >({
    query: `
      mutation UpdateAppearanceSettings($input: UpdateAppearanceSettingsInput!) {
        updateAppearanceSettings(input: $input) {
          ${appearanceFields}
        }
      }
    `,
    variables: {
      input: {
        backgroundType: settings.backgroundType.toUpperCase(),
        solidTheme: settings.solidTheme.toUpperCase(),
        backgroundMask: settings.backgroundMask,
        wallpaperColorScheme: settings.wallpaperColorScheme.toUpperCase(),
      },
    },
  });
  return normalizeAppearanceSettings(data.updateAppearanceSettings);
}

export async function uploadAppearanceWallpaper(file: File) {
  const body = new FormData();
  body.append(
    'operations',
    JSON.stringify({
      query: `
        mutation UploadAppearanceWallpaper($file: Upload!) {
          uploadAppearanceWallpaper(file: $file) {
            ${appearanceFields}
          }
        }
      `,
      variables: { file: null },
    }),
  );
  body.append('map', JSON.stringify({ '0': ['variables.file'] }));
  body.append('0', file, file.name);
  const data = await graphqlMultipartFetch<{
    uploadAppearanceWallpaper: RawAppearanceSettings;
  }>(body);
  return normalizeAppearanceSettings(data.uploadAppearanceWallpaper);
}

interface RawAppearanceSettings {
  backgroundType: string;
  solidTheme: string;
  backgroundMask: number;
  wallpaperColorScheme: string;
  wallpaperId: string;
  wallpaperFilename: string;
}

function normalizeAppearanceSettings(settings: RawAppearanceSettings): AppearanceSettings {
  const backgroundType = settings.backgroundType.toLowerCase();
  const solidTheme = settings.solidTheme.toLowerCase();
  const scheme = settings.wallpaperColorScheme.toLowerCase();
  return {
    backgroundType: isBackgroundType(backgroundType) ? backgroundType : 'bing',
    solidTheme: isSolidTheme(solidTheme) ? solidTheme : 'vermilion',
    backgroundMask: Math.min(100, Math.max(0, Math.round(settings.backgroundMask))),
    wallpaperColorScheme: isWallpaperColorScheme(scheme) ? scheme : 'content',
    wallpaperId: settings.wallpaperId,
    wallpaperFilename: settings.wallpaperFilename,
  };
}

function isBackgroundType(value: string): value is AppearanceBackgroundType {
  return value === 'solid' || value === 'image' || value === 'bing' || value === 'nasa';
}
