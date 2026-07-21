import {
  Hct,
  QuantizerCelebi,
  SchemeContent,
  SchemeExpressive,
  SchemeFidelity,
  SchemeFruitSalad,
  SchemeMonochrome,
  SchemeNeutral,
  SchemeRainbow,
  SchemeTonalSpot,
  SchemeVibrant,
  Score,
  argbFromHex,
  argbFromRgb,
  hexFromArgb,
} from '@material/material-color-utilities';

export const dailyBackgroundStorageKey = 'anycode.theme.daily-background.v2';

export const wallpaperColorSchemes = [
  'content',
  'fidelity',
  'tonal_spot',
  'vibrant',
  'expressive',
  'rainbow',
  'fruit_salad',
  'neutral',
  'monochrome',
];

export const materialPaletteKeys = [
  'background',
  'onBackground',
  'surface',
  'surfaceDim',
  'surfaceBright',
  'surfaceContainerLowest',
  'surfaceContainerLow',
  'surfaceContainer',
  'surfaceContainerHigh',
  'surfaceContainerHighest',
  'onSurface',
  'surfaceVariant',
  'onSurfaceVariant',
  'inverseSurface',
  'inverseOnSurface',
  'outline',
  'outlineVariant',
  'shadow',
  'scrim',
  'surfaceTint',
  'primary',
  'onPrimary',
  'primaryContainer',
  'onPrimaryContainer',
  'inversePrimary',
  'primaryFixed',
  'primaryFixedDim',
  'onPrimaryFixed',
  'onPrimaryFixedVariant',
  'secondary',
  'onSecondary',
  'secondaryContainer',
  'onSecondaryContainer',
  'secondaryFixed',
  'secondaryFixedDim',
  'onSecondaryFixed',
  'onSecondaryFixedVariant',
  'tertiary',
  'onTertiary',
  'tertiaryContainer',
  'onTertiaryContainer',
  'tertiaryFixed',
  'tertiaryFixedDim',
  'onTertiaryFixed',
  'onTertiaryFixedVariant',
  'error',
  'onError',
  'errorContainer',
  'onErrorContainer',
];

const schemeConstructors = {
  content: SchemeContent,
  fidelity: SchemeFidelity,
  tonal_spot: SchemeTonalSpot,
  vibrant: SchemeVibrant,
  expressive: SchemeExpressive,
  rainbow: SchemeRainbow,
  fruit_salad: SchemeFruitSalad,
  neutral: SchemeNeutral,
  monochrome: SchemeMonochrome,
};

const fallbackSourceColor = '#4285f4';
const hexPattern = /^#[0-9a-f]{6}$/;
const shaPattern = /^[0-9a-f]{64}$/;
const allowedImageHosts = new Set(['cn.bing.com', 'www.bing.com']);

export function isWallpaperColorScheme(value) {
  return wallpaperColorSchemes.includes(value);
}

export function parseDailyMetadata(input) {
  if (!isObject(input) || String(input.status) !== '200') return null;
  if (!validCalendarDate(input.date, '/')) return null;
  const imageUrl = normalizeBingImageUrl(input.imgurl);
  const title = nonEmptyString(input.imgtitle);
  const copyright = nonEmptyString(input.imgcopyright);
  if (!imageUrl || !title || !copyright) return null;
  return { sourceDate: input.date, imageUrl, title, copyright };
}

export function parseDailyBackgroundRecord(input) {
  let value = input;
  if (typeof value === 'string') {
    try {
      value = JSON.parse(value);
    } catch {
      return null;
    }
  }
  if (!isObject(value) || value.version !== 2 || value.source !== 'ee123') return null;
  const imageUrl = normalizeBingImageUrl(value.imageUrl);
  if (
    !validCalendarDate(value.localDate, '-') ||
    !validCalendarDate(value.sourceDate, '/') ||
    !imageUrl ||
    !shaPattern.test(value.sha256) ||
    !validTimestamp(value.checkedAt) ||
    !validTimestamp(value.loadedAt) ||
    !isObject(value.attribution) ||
    !nonEmptyString(value.attribution.title) ||
    !nonEmptyString(value.attribution.copyright) ||
    value.attribution.sourceUrl !== 'https://bing.ee123.net/' ||
    typeof value.sourceColor !== 'string' ||
    !hexPattern.test(value.sourceColor) ||
    !isObject(value.colorCache) ||
    value.colorCache.sha256 !== value.sha256 ||
    !isWallpaperColorScheme(value.colorCache.wallpaperColorScheme) ||
    !isObject(value.colorCache.palettes) ||
    !validPalette(value.colorCache.palettes.light) ||
    !validPalette(value.colorCache.palettes.dark)
  ) {
    return null;
  }
  return {
    version: 2,
    source: 'ee123',
    localDate: value.localDate,
    sourceDate: value.sourceDate,
    imageUrl,
    sha256: value.sha256,
    checkedAt: value.checkedAt,
    loadedAt: value.loadedAt,
    attribution: {
      title: value.attribution.title.trim(),
      copyright: value.attribution.copyright.trim(),
      sourceUrl: 'https://bing.ee123.net/',
    },
    sourceColor: value.sourceColor,
    colorCache: {
      sha256: value.colorCache.sha256,
      wallpaperColorScheme: value.colorCache.wallpaperColorScheme,
      palettes: {
        light: pickPalette(value.colorCache.palettes.light),
        dark: pickPalette(value.colorCache.palettes.dark),
      },
    },
  };
}

export function dailyImageRefreshReason(record, metadata, localDate) {
  if (!record) return 'missing-record';
  if (!validCalendarDate(localDate, '-') || record.localDate !== localDate) return 'local-date';
  if (record.sourceDate !== metadata.sourceDate) return 'source-date';
  if (record.imageUrl !== metadata.imageUrl) return 'image-url';
  return null;
}

export function extractSourceColor(data) {
  const pixels = [];
  // GLUE: ImageData exposes RGBA bytes while Material's official quantizer accepts packed ARGB values.
  for (let index = 0; index + 3 < data.length; index += 4) {
    if (data[index + 3] < 255) continue;
    pixels.push(argbFromRgb(data[index], data[index + 1], data[index + 2]));
  }
  const quantized = QuantizerCelebi.quantize(pixels, 128);
  return hexFromArgb(Score.score(quantized)[0]);
}

export function createMaterialPalettes(sourceColor, schemeName) {
  const normalizedSource = hexPattern.test(sourceColor) ? sourceColor : fallbackSourceColor;
  const SchemeConstructor = schemeConstructors[schemeName] ?? SchemeContent;
  const sourceHct = Hct.fromInt(argbFromHex(normalizedSource));
  return {
    light: materialPalette(new SchemeConstructor(sourceHct, false, 0)),
    dark: materialPalette(new SchemeConstructor(sourceHct, true, 0)),
  };
}

export function resolveMaterialPaletteCache(sha256, sourceColor, cache, schemeName) {
  if (cache?.sha256 === sha256 && cache.wallpaperColorScheme === schemeName) return cache;
  return {
    sha256,
    wallpaperColorScheme: schemeName,
    palettes: createMaterialPalettes(sourceColor, schemeName),
  };
}

export function contrastRatio(foreground, background) {
  const lighter = Math.max(relativeLuminance(foreground), relativeLuminance(background));
  const darker = Math.min(relativeLuminance(foreground), relativeLuminance(background));
  return (lighter + 0.05) / (darker + 0.05);
}

function materialPalette(scheme) {
  return Object.fromEntries(materialPaletteKeys.map((key) => [key, hexFromArgb(scheme[key])]));
}

function validPalette(value) {
  return isObject(value) && materialPaletteKeys.every((key) => hexPattern.test(value[key]));
}

function pickPalette(value) {
  return Object.fromEntries(materialPaletteKeys.map((key) => [key, value[key]]));
}

function normalizeBingImageUrl(value) {
  if (typeof value !== 'string') return '';
  try {
    const url = new URL(value);
    return url.protocol === 'https:' && allowedImageHosts.has(url.hostname) ? url.href : '';
  } catch {
    return '';
  }
}

function relativeLuminance(color) {
  const channels = [1, 3, 5].map((start) => Number.parseInt(color.slice(start, start + 2), 16));
  const linear = channels.map((channel) => {
    const value = channel / 255;
    return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
  });
  return linear[0] * 0.2126 + linear[1] * 0.7152 + linear[2] * 0.0722;
}

function validTimestamp(value) {
  if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/.test(value)) {
    return false;
  }
  try {
    return new Date(value).toISOString() === value;
  } catch {
    return false;
  }
}

function validCalendarDate(value, separator) {
  if (typeof value !== 'string') return false;
  const pattern = separator === '-' ? /^(\d{4})-(\d{2})-(\d{2})$/ : /^(\d{4})\/(\d{2})\/(\d{2})$/;
  const match = pattern.exec(value);
  if (!match) return false;
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  const leapYear = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const daysInMonth = [31, leapYear ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return month >= 1 && month <= 12 && day >= 1 && day <= daysInMonth[month - 1];
}

function nonEmptyString(value) {
  return typeof value === 'string' && value.trim() ? value.trim() : '';
}

function isObject(value) {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}
