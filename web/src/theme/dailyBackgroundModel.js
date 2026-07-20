export const dailyBackgroundStorageKey = 'anycode.theme.daily-background.v1';

export const dailyPaletteKeys = [
  'primary',
  'primaryHover',
  'onPrimary',
  'link',
  'focusRing',
  'page',
  'surface',
  'surfaceMuted',
  'surfaceRaised',
  'surfaceHover',
  'surfaceSelected',
  'border',
  'borderStrong',
  'text',
  'textMuted',
];

const fallbackAccent = '#2563eb';
const hexPattern = /^#[0-9a-f]{6}$/;
const shaPattern = /^[0-9a-f]{64}$/;
const allowedImageHosts = new Set(['cn.bing.com', 'www.bing.com']);

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
  if (!isObject(value) || value.version !== 1 || value.source !== 'ee123') return null;
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
    !isObject(value.seed) ||
    !hexPattern.test(value.seed.dominant) ||
    !hexPattern.test(value.seed.accent) ||
    !isObject(value.palettes) ||
    !validPalette(value.palettes.light) ||
    !validPalette(value.palettes.dark)
  ) {
    return null;
  }
  return {
    version: 1,
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
    seed: { dominant: value.seed.dominant, accent: value.seed.accent },
    palettes: { light: pickPalette(value.palettes.light), dark: pickPalette(value.palettes.dark) },
  };
}

export function dailyImageRefreshReason(record, metadata, localDate) {
  if (!record) return 'missing-record';
  if (!validCalendarDate(localDate, '-') || record.localDate !== localDate) return 'local-date';
  if (record.sourceDate !== metadata.sourceDate) return 'source-date';
  if (record.imageUrl !== metadata.imageUrl) return 'image-url';
  return null;
}

export function extractSeedColors(data) {
  const buckets = new Map();
  for (let index = 0; index + 3 < data.length; index += 4) {
    if (data[index + 3] < 128) continue;
    const red = data[index];
    const green = data[index + 1];
    const blue = data[index + 2];
    const key = (red >> 3) * 1024 + (green >> 3) * 32 + (blue >> 3);
    const bucket = buckets.get(key) ?? { count: 0, red: 0, green: 0, blue: 0 };
    bucket.count += 1;
    bucket.red += red;
    bucket.green += green;
    bucket.blue += blue;
    buckets.set(key, bucket);
  }
  const colors = [...buckets.values()]
    .sort((left, right) => right.count - left.count)
    .slice(0, 8)
    .map((bucket) => {
      const rgb = {
        red: Math.round(bucket.red / bucket.count),
        green: Math.round(bucket.green / bucket.count),
        blue: Math.round(bucket.blue / bucket.count),
      };
      const hsl = rgbToHsl(rgb);
      return { ...bucket, rgb, hsl };
    });
  if (!colors.length) return { dominant: fallbackAccent, accent: fallbackAccent };
  const dominant = rgbToHex(colors[0].rgb);
  const accent = colors
    .filter(({ hsl }) => hsl.saturation >= 0.18 && hsl.lightness >= 0.08 && hsl.lightness <= 0.92)
    .sort((left, right) => accentScore(right) - accentScore(left))[0];
  return { dominant, accent: accent ? rgbToHex(accent.rgb) : dominant };
}

export function createDailyPalettes(seed) {
  const source = hexPattern.test(seed.accent) ? seed.accent : fallbackAccent;
  const accent = rgbToHsl(hexToRgb(source));
  const hue = accent.hue;
  const saturation = clamp(accent.saturation, 0.4, 0.72);

  const lightSurface = hslToHex(hue, 0.18, 0.99);
  const lightPage = hslToHex(hue, 0.22, 0.965);
  const lightText = ensureContrast(hue, 0.28, 0.12, lightSurface, 4.5, 'darker');
  const lightMuted = ensureContrast(hue, 0.16, 0.34, lightSurface, 4.5, 'darker');
  const lightPrimary = ensureContrast(hue, saturation, 0.45, lightSurface, 3, 'darker');
  const lightLink = ensureContrast(hue, saturation, 0.4, lightSurface, 4.5, 'darker');

  const darkSurface = hslToHex(hue, 0.24, 0.105);
  const darkPage = hslToHex(hue, 0.28, 0.07);
  const darkText = ensureContrast(hue, 0.16, 0.92, darkSurface, 4.5, 'lighter');
  const darkMuted = ensureContrast(hue, 0.12, 0.72, darkSurface, 4.5, 'lighter');
  const darkPrimary = ensureContrast(hue, saturation, 0.64, darkSurface, 3, 'lighter');
  const darkLink = ensureContrast(hue, saturation, 0.72, darkSurface, 4.5, 'lighter');

  return {
    light: {
      primary: lightPrimary,
      primaryHover: shiftLightness(lightPrimary, -0.07),
      onPrimary: bestContrastingText(lightPrimary),
      link: lightLink,
      focusRing: ensureContrast(hue, saturation, 0.4, lightSurface, 3, 'darker'),
      page: lightPage,
      surface: lightSurface,
      surfaceMuted: hslToHex(hue, 0.2, 0.935),
      surfaceRaised: hslToHex(hue, 0.14, 1),
      surfaceHover: hslToHex(hue, 0.32, 0.93),
      surfaceSelected: hslToHex(hue, 0.48, 0.875),
      border: hslToHex(hue, 0.18, 0.75),
      borderStrong: ensureContrast(hue, 0.18, 0.43, lightSurface, 3, 'darker'),
      text: lightText,
      textMuted: lightMuted,
    },
    dark: {
      primary: darkPrimary,
      primaryHover: shiftLightness(darkPrimary, 0.07),
      onPrimary: bestContrastingText(darkPrimary),
      link: darkLink,
      focusRing: ensureContrast(hue, saturation, 0.64, darkSurface, 3, 'lighter'),
      page: darkPage,
      surface: darkSurface,
      surfaceMuted: hslToHex(hue, 0.28, 0.08),
      surfaceRaised: hslToHex(hue, 0.26, 0.145),
      surfaceHover: hslToHex(hue, 0.3, 0.18),
      surfaceSelected: hslToHex(hue, 0.44, 0.25),
      border: hslToHex(hue, 0.18, 0.34),
      borderStrong: ensureContrast(hue, 0.16, 0.56, darkSurface, 3, 'lighter'),
      text: darkText,
      textMuted: darkMuted,
    },
  };
}

export function contrastRatio(foreground, background) {
  const lighter = Math.max(relativeLuminance(foreground), relativeLuminance(background));
  const darker = Math.min(relativeLuminance(foreground), relativeLuminance(background));
  return (lighter + 0.05) / (darker + 0.05);
}

function validPalette(value) {
  return isObject(value) && dailyPaletteKeys.every((key) => hexPattern.test(value[key]));
}

function pickPalette(value) {
  return Object.fromEntries(dailyPaletteKeys.map((key) => [key, value[key]]));
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

function accentScore(color) {
  const middleWeight = 1 - Math.abs(color.hsl.lightness - 0.5) * 0.8;
  return color.count * (0.35 + color.hsl.saturation) * middleWeight;
}

function ensureContrast(hue, saturation, initialLightness, background, minimum, direction) {
  const step = direction === 'lighter' ? 0.01 : -0.01;
  let lightness = initialLightness;
  for (let attempt = 0; attempt <= 100; attempt += 1) {
    const candidate = hslToHex(hue, saturation, lightness);
    if (contrastRatio(candidate, background) >= minimum) return candidate;
    lightness = clamp(lightness + step, 0, 1);
  }
  return direction === 'lighter' ? '#ffffff' : '#000000';
}

function bestContrastingText(background) {
  return contrastRatio('#ffffff', background) >= contrastRatio('#0f172a', background)
    ? '#ffffff'
    : '#0f172a';
}

function shiftLightness(color, amount) {
  const hsl = rgbToHsl(hexToRgb(color));
  return hslToHex(hsl.hue, hsl.saturation, clamp(hsl.lightness + amount, 0, 1));
}

function relativeLuminance(color) {
  const { red, green, blue } = hexToRgb(color);
  const linear = [red, green, blue].map((channel) => {
    const value = channel / 255;
    return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
  });
  return linear[0] * 0.2126 + linear[1] * 0.7152 + linear[2] * 0.0722;
}

function rgbToHsl({ red, green, blue }) {
  const r = red / 255;
  const g = green / 255;
  const b = blue / 255;
  const maximum = Math.max(r, g, b);
  const minimum = Math.min(r, g, b);
  const delta = maximum - minimum;
  const lightness = (maximum + minimum) / 2;
  if (delta === 0) return { hue: 217 / 360, saturation: 0, lightness };
  const saturation = delta / (1 - Math.abs(2 * lightness - 1));
  let hue;
  if (maximum === r) hue = ((g - b) / delta) % 6;
  else if (maximum === g) hue = (b - r) / delta + 2;
  else hue = (r - g) / delta + 4;
  return { hue: ((hue * 60 + 360) % 360) / 360, saturation, lightness };
}

function hslToHex(hue, saturation, lightness) {
  const h = ((hue % 1) + 1) % 1;
  const chroma = (1 - Math.abs(2 * lightness - 1)) * saturation;
  const sector = h * 6;
  const intermediate = chroma * (1 - Math.abs((sector % 2) - 1));
  let channels;
  if (sector < 1) channels = [chroma, intermediate, 0];
  else if (sector < 2) channels = [intermediate, chroma, 0];
  else if (sector < 3) channels = [0, chroma, intermediate];
  else if (sector < 4) channels = [0, intermediate, chroma];
  else if (sector < 5) channels = [intermediate, 0, chroma];
  else channels = [chroma, 0, intermediate];
  const offset = lightness - chroma / 2;
  return rgbToHex({
    red: Math.round((channels[0] + offset) * 255),
    green: Math.round((channels[1] + offset) * 255),
    blue: Math.round((channels[2] + offset) * 255),
  });
}

function hexToRgb(color) {
  return {
    red: Number.parseInt(color.slice(1, 3), 16),
    green: Number.parseInt(color.slice(3, 5), 16),
    blue: Number.parseInt(color.slice(5, 7), 16),
  };
}

function rgbToHex({ red, green, blue }) {
  return `#${[red, green, blue]
    .map((channel) => clamp(Math.round(channel), 0, 255).toString(16).padStart(2, '0'))
    .join('')}`;
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

function clamp(value, minimum, maximum) {
  return Math.min(maximum, Math.max(minimum, value));
}
