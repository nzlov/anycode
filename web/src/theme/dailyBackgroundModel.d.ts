// GLUE: Node's built-in test runner imports the executable JS model; remove when the test toolchain executes TypeScript directly.
export type WallpaperColorScheme =
  | 'content'
  | 'fidelity'
  | 'tonal_spot'
  | 'vibrant'
  | 'expressive'
  | 'rainbow'
  | 'fruit_salad'
  | 'neutral'
  | 'monochrome';

export type MaterialPalette = Record<
  | 'background'
  | 'onBackground'
  | 'surface'
  | 'surfaceDim'
  | 'surfaceBright'
  | 'surfaceContainerLowest'
  | 'surfaceContainerLow'
  | 'surfaceContainer'
  | 'surfaceContainerHigh'
  | 'surfaceContainerHighest'
  | 'onSurface'
  | 'surfaceVariant'
  | 'onSurfaceVariant'
  | 'inverseSurface'
  | 'inverseOnSurface'
  | 'outline'
  | 'outlineVariant'
  | 'shadow'
  | 'scrim'
  | 'surfaceTint'
  | 'primary'
  | 'onPrimary'
  | 'primaryContainer'
  | 'onPrimaryContainer'
  | 'inversePrimary'
  | 'primaryFixed'
  | 'primaryFixedDim'
  | 'onPrimaryFixed'
  | 'onPrimaryFixedVariant'
  | 'secondary'
  | 'onSecondary'
  | 'secondaryContainer'
  | 'onSecondaryContainer'
  | 'secondaryFixed'
  | 'secondaryFixedDim'
  | 'onSecondaryFixed'
  | 'onSecondaryFixedVariant'
  | 'tertiary'
  | 'onTertiary'
  | 'tertiaryContainer'
  | 'onTertiaryContainer'
  | 'tertiaryFixed'
  | 'tertiaryFixedDim'
  | 'onTertiaryFixed'
  | 'onTertiaryFixedVariant'
  | 'error'
  | 'onError'
  | 'errorContainer'
  | 'onErrorContainer',
  string
>;

export interface DailyMetadata {
  sourceDate: string;
  imageUrl: string;
  title: string;
  copyright: string;
}

export interface DailyBackgroundRecord {
  version: 2;
  source: 'ee123';
  localDate: string;
  sourceDate: string;
  imageUrl: string;
  sha256: string;
  checkedAt: string;
  loadedAt: string;
  attribution: { title: string; copyright: string; sourceUrl: 'https://bing.ee123.net/' };
  sourceColor: string;
  colorCache: MaterialPaletteCache;
}

export interface MaterialPaletteCache {
  sha256: string;
  wallpaperColorScheme: WallpaperColorScheme;
  palettes: { light: MaterialPalette; dark: MaterialPalette };
}

export const dailyBackgroundStorageKey: 'anycode.theme.daily-background.v2';
export const wallpaperColorSchemes: WallpaperColorScheme[];
export const materialPaletteKeys: Array<keyof MaterialPalette>;
export function isWallpaperColorScheme(value: unknown): value is WallpaperColorScheme;
export function parseDailyMetadata(input: unknown): DailyMetadata | null;
export function parseDailyBackgroundRecord(input: unknown): DailyBackgroundRecord | null;
export function dailyImageRefreshReason(
  record: DailyBackgroundRecord | null,
  metadata: DailyMetadata,
  localDate: string,
): 'missing-record' | 'local-date' | 'source-date' | 'image-url' | null;
export function extractSourceColor(data: Uint8ClampedArray): string;
export function createMaterialPalettes(
  sourceColor: string,
  schemeName: WallpaperColorScheme,
): { light: MaterialPalette; dark: MaterialPalette };
export function resolveMaterialPaletteCache(
  sha256: string,
  sourceColor: string,
  cache: MaterialPaletteCache | null,
  schemeName: WallpaperColorScheme,
): MaterialPaletteCache;
export function contrastRatio(foreground: string, background: string): number;
