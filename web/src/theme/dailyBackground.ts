import { readonly, shallowRef } from 'vue';

import {
  createDailyPalettes,
  dailyBackgroundStorageKey,
  dailyImageRefreshReason,
  dailyPaletteKeys,
  extractSeedColors,
  parseDailyBackgroundRecord,
  parseDailyMetadata,
  type DailyBackgroundRecord,
  type DailyMetadata,
  type DailyPalette,
} from '@/theme/dailyBackgroundModel';

const metadataEndpoint = 'https://bing.ee123.net/img/?size=UHD&imgtype=jpg&type=json';
const imageEndpoint = 'https://bing.ee123.net/img/4k';
const attributionSource = 'https://bing.ee123.net/' as const;

interface DailyBackgroundView {
  title: string;
  copyright: string;
  sourceUrl: string;
}

const currentBackground = shallowRef<DailyBackgroundView | null>(null);
export const dailyBackgroundState = readonly(currentBackground);

let initialized = false;

export function initializeDailyBackground() {
  if (initialized) return;
  initialized = true;
  const cached = readCachedRecord();
  if (cached) applyRecord(cached);
  void refreshDailyBackground(cached);
}

async function refreshDailyBackground(cached: DailyBackgroundRecord | null) {
  try {
    const metadata = await fetchMetadata();
    const checkedAt = new Date().toISOString();
    const localDate = currentLocalDate();
    if (!dailyImageRefreshReason(cached, metadata, localDate) && cached) {
      commitRecord({
        ...cached,
        checkedAt,
        attribution: attributionFrom(metadata),
      });
      return;
    }

    const image = await fetchImage(metadata);
    const sha256 = await sha256Hex(image.bytes);
    const loadedAt = new Date().toISOString();
    const sameImage = cached?.sha256 === sha256;
    const seed = sameImage ? cached.seed : await extractImageSeed(image.bytes, image.mimeType);
    const palettes = sameImage ? cached.palettes : createDailyPalettes(seed);
    commitRecord({
      version: 1,
      source: 'ee123',
      localDate,
      sourceDate: metadata.sourceDate,
      imageUrl: metadata.imageUrl,
      sha256,
      checkedAt,
      loadedAt,
      attribution: attributionFrom(metadata),
      seed,
      palettes,
    });
  } catch {
    // The already-applied record or static CSS tokens remain the complete fallback.
  }
}

async function fetchMetadata(): Promise<DailyMetadata> {
  const response = await fetch(metadataEndpoint, { cache: 'no-store', mode: 'cors' });
  if (!response.ok) throw new Error(`daily image metadata returned ${response.status}`);
  const metadata = parseDailyMetadata(await response.json());
  if (!metadata) throw new Error('daily image metadata is invalid');
  return metadata;
}

async function fetchImage(metadata: DailyMetadata) {
  const response = await fetch(imageEndpoint, {
    cache: 'no-cache',
    mode: 'cors',
    redirect: 'follow',
  });
  const mimeType = response.headers.get('content-type')?.split(';', 1)[0]?.trim().toLowerCase() ?? '';
  if (!response.ok || !['image/jpeg', 'image/webp'].includes(mimeType)) {
    throw new Error(`daily image returned ${response.status} ${mimeType}`);
  }
  if (new URL(response.url).href !== metadata.imageUrl) {
    throw new Error('daily image changed while loading');
  }
  return { bytes: await response.arrayBuffer(), mimeType };
}

async function sha256Hex(bytes: ArrayBuffer) {
  const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', bytes));
  return [...digest].map((value) => value.toString(16).padStart(2, '0')).join('');
}

async function extractImageSeed(bytes: ArrayBuffer, mimeType: string) {
  const bitmap = await createImageBitmap(new Blob([bytes], { type: mimeType }));
  try {
    const scale = Math.min(64 / bitmap.width, 36 / bitmap.height, 1);
    const width = Math.max(1, Math.round(bitmap.width * scale));
    const height = Math.max(1, Math.round(bitmap.height * scale));
    const canvas = document.createElement('canvas');
    canvas.width = width;
    canvas.height = height;
    const context = canvas.getContext('2d', { willReadFrequently: true });
    if (!context) throw new Error('daily image canvas is unavailable');
    context.drawImage(bitmap, 0, 0, width, height);
    return extractSeedColors(context.getImageData(0, 0, width, height).data);
  } finally {
    bitmap.close();
  }
}

function readCachedRecord() {
  try {
    return parseDailyBackgroundRecord(window.localStorage.getItem(dailyBackgroundStorageKey));
  } catch {
    return null;
  }
}

function commitRecord(record: DailyBackgroundRecord) {
  const validated = parseDailyBackgroundRecord(record);
  if (!validated) throw new Error('daily background record is invalid');
  applyRecord(validated);
  try {
    window.localStorage.setItem(dailyBackgroundStorageKey, JSON.stringify(validated));
  } catch {
    // The in-memory theme remains usable for this visit when storage is unavailable.
  }
}

function applyRecord(record: DailyBackgroundRecord) {
  const root = document.documentElement;
  applyPalette(root, 'light', record.palettes.light);
  applyPalette(root, 'dark', record.palettes.dark);
  root.style.setProperty('--ac-daily-background-image', `url(${JSON.stringify(record.imageUrl)})`);
  root.dataset.dailyBackground = 'ready';
  currentBackground.value = {
    title: record.attribution.title,
    copyright: record.attribution.copyright,
    sourceUrl: record.attribution.sourceUrl,
  };
}

function applyPalette(root: HTMLElement, mode: 'light' | 'dark', palette: DailyPalette) {
  for (const key of dailyPaletteKeys) {
    root.style.setProperty(`--ac-daily-${mode}-${toKebabCase(key)}`, palette[key]);
  }
}

function attributionFrom(metadata: DailyMetadata) {
  return {
    title: metadata.title,
    copyright: metadata.copyright,
    sourceUrl: attributionSource,
  };
}

function currentLocalDate() {
  const now = new Date();
  const year = String(now.getFullYear()).padStart(4, '0');
  const month = String(now.getMonth() + 1).padStart(2, '0');
  const day = String(now.getDate()).padStart(2, '0');
  return `${year}-${month}-${day}`;
}

function toKebabCase(value: string) {
  return value.replace(/[A-Z]/g, (letter) => `-${letter.toLowerCase()}`);
}
