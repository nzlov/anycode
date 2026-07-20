// GLUE: Node's built-in test runner imports the executable JS model; remove when the test toolchain executes TypeScript directly.
export type DailyPalette = Record<
  | 'primary'
  | 'primaryHover'
  | 'onPrimary'
  | 'link'
  | 'focusRing'
  | 'page'
  | 'surface'
  | 'surfaceMuted'
  | 'surfaceRaised'
  | 'surfaceHover'
  | 'surfaceSelected'
  | 'border'
  | 'borderStrong'
  | 'text'
  | 'textMuted',
  string
>;

export interface DailyMetadata {
  sourceDate: string;
  imageUrl: string;
  title: string;
  copyright: string;
}

export interface DailyBackgroundRecord {
  version: 1;
  source: 'ee123';
  localDate: string;
  sourceDate: string;
  imageUrl: string;
  sha256: string;
  checkedAt: string;
  loadedAt: string;
  attribution: { title: string; copyright: string; sourceUrl: 'https://bing.ee123.net/' };
  seed: { dominant: string; accent: string };
  palettes: { light: DailyPalette; dark: DailyPalette };
}

export const dailyBackgroundStorageKey: 'anycode.theme.daily-background.v1';
export const dailyPaletteKeys: Array<keyof DailyPalette>;
export function parseDailyMetadata(input: unknown): DailyMetadata | null;
export function parseDailyBackgroundRecord(input: unknown): DailyBackgroundRecord | null;
export function dailyImageRefreshReason(
  record: DailyBackgroundRecord | null,
  metadata: DailyMetadata,
  localDate: string,
): 'missing-record' | 'local-date' | 'source-date' | 'image-url' | null;
export function extractSeedColors(data: Uint8ClampedArray): { dominant: string; accent: string };
export function createDailyPalettes(seed: { dominant: string; accent: string }): {
  light: DailyPalette;
  dark: DailyPalette;
};
export function contrastRatio(foreground: string, background: string): number;
