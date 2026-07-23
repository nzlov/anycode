import { getGraphQLAccessKey } from '@/services/graphqlClient';
import {
  createMaterialPalettes,
  dailyBackgroundStorageKey,
  dailyImageRefreshReason,
  extractSourceColor,
  isWallpaperColorScheme,
  materialPaletteKeys,
  parseDailyBackgroundRecord,
  parseDailyMetadata,
  resolveMaterialPaletteCache,
  type DailyBackgroundRecord,
  type DailyMetadata,
  type MaterialPalette,
  type WallpaperColorScheme,
} from '@/theme/dailyBackgroundModel';

const metadataEndpoint = 'https://bing.ee123.net/img/?size=UHD&imgtype=jpg&type=json';
const imageEndpoint = 'https://bing.ee123.net/img/4k';
const attributionSource = 'https://bing.ee123.net/' as const;

type BackgroundSource = 'solid' | 'image' | 'bing';

let activeSource: BackgroundSource | null = null;
let activeRecord: DailyBackgroundRecord | null = null;
let activeScheme: WallpaperColorScheme = 'content';
let requestVersion = 0;
let uploadedObjectURL = '';

export function initializeDailyBackground() {
  activateBingBackground('content');
}

export function activateBingBackground(scheme: WallpaperColorScheme) {
  if (!isWallpaperColorScheme(scheme)) return;
  activeSource = 'bing';
  activeScheme = scheme;
  const version = ++requestVersion;
  const cached = readCachedRecord();
  if (cached) commitRecord(cached, false);
  void refreshDailyBackground(cached, version);
}

export function activateSolidBackground(sourceColor: string) {
  activeSource = 'solid';
  requestVersion += 1;
  releaseUploadedObjectURL();
  applyPalettes(createMaterialPalettes(sourceColor, 'tonal_spot'));
  applyBackground({ color: sourceColor, image: 'none' });
}

export async function activateUploadedBackground(id: string, scheme: WallpaperColorScheme) {
  if (!id || !isWallpaperColorScheme(scheme)) return;
  activeSource = 'image';
  activeScheme = scheme;
  const version = ++requestVersion;
  try {
    const response = await fetch(`/api/appearance/wallpapers/${encodeURIComponent(id)}`, {
      cache: 'no-cache',
      headers: authorizationHeaders(),
    });
    const mimeType = response.headers.get('content-type')?.split(';', 1)[0]?.trim() ?? '';
    if (!response.ok || !['image/jpeg', 'image/png'].includes(mimeType)) {
      throw new Error(`uploaded wallpaper returned ${response.status} ${mimeType}`);
    }
    const bytes = await response.arrayBuffer();
    const sourceColor = await extractImageSourceColor(bytes, mimeType);
    if (activeSource !== 'image' || version !== requestVersion) return;
    const objectURL = URL.createObjectURL(new Blob([bytes], { type: mimeType }));
    releaseUploadedObjectURL();
    uploadedObjectURL = objectURL;
    applyPalettes(createMaterialPalettes(sourceColor, scheme));
    applyBackground({ image: `url(${JSON.stringify(objectURL)})` });
  } catch {
    // The previous complete background remains visible when the upload cannot be loaded.
  }
}

export function setWallpaperColorScheme(scheme: WallpaperColorScheme) {
  if (!isWallpaperColorScheme(scheme)) return;
  activeScheme = scheme;
  if (activeSource === 'bing' && activeRecord) commitRecord(activeRecord);
}

export function setBackgroundMask(opacity: number) {
  const normalized = Math.min(100, Math.max(0, Math.round(opacity)));
  document.documentElement.style.setProperty(
    '--ac-background-mask-opacity',
    String(normalized / 100),
  );
}

async function refreshDailyBackground(cached: DailyBackgroundRecord | null, version: number) {
  try {
    const metadata = await fetchMetadata();
    const checkedAt = new Date().toISOString();
    const localDate = currentLocalDate();
    if (!dailyImageRefreshReason(cached, metadata, localDate) && cached) {
      if (activeSource !== 'bing' || version !== requestVersion) return;
      commitRecord({ ...cached, checkedAt, attribution: attributionFrom(metadata) });
      return;
    }

    const image = await fetchImage(metadata);
    const sha256 = await sha256Hex(image.bytes);
    const loadedAt = new Date().toISOString();
    const sameImage = cached?.sha256 === sha256;
    const sourceColor =
      sameImage && cached
        ? cached.sourceColor
        : await extractImageSourceColor(image.bytes, image.mimeType);
    if (activeSource !== 'bing' || version !== requestVersion) return;
    const colorCache =
      sameImage && cached
        ? cached.colorCache
        : {
            sha256,
            wallpaperColorScheme: activeScheme,
            palettes: createMaterialPalettes(sourceColor, activeScheme),
          };
    commitRecord({
      version: 2,
      source: 'ee123',
      localDate,
      sourceDate: metadata.sourceDate,
      imageUrl: metadata.imageUrl,
      sha256,
      checkedAt,
      loadedAt,
      attribution: attributionFrom(metadata),
      sourceColor,
      colorCache,
    });
  } catch {
    // The already-applied background or static CSS tokens remain the complete fallback.
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
  const mimeType =
    response.headers.get('content-type')?.split(';', 1)[0]?.trim().toLowerCase() ?? '';
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

async function extractImageSourceColor(bytes: ArrayBuffer, mimeType: string) {
  const bitmap = await createImageBitmap(new Blob([bytes], { type: mimeType }));
  try {
    const scale = Math.min(64 / bitmap.width, 36 / bitmap.height, 1);
    const width = Math.max(1, Math.round(bitmap.width * scale));
    const height = Math.max(1, Math.round(bitmap.height * scale));
    const canvas = document.createElement('canvas');
    canvas.width = width;
    canvas.height = height;
    const context = canvas.getContext('2d', { willReadFrequently: true });
    if (!context) throw new Error('background image canvas is unavailable');
    context.drawImage(bitmap, 0, 0, width, height);
    return extractSourceColor(context.getImageData(0, 0, width, height).data);
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

function commitRecord(record: DailyBackgroundRecord, persist = true) {
  const validated = parseDailyBackgroundRecord(withCurrentPaletteCache(record));
  if (!validated) throw new Error('daily background record is invalid');
  activeRecord = validated;
  if (activeSource === 'bing') releaseUploadedObjectURL();
  applyPalettes(validated.colorCache.palettes);
  applyBackground({ image: `url(${JSON.stringify(validated.imageUrl)})` });
  if (!persist) return;
  try {
    window.localStorage.setItem(dailyBackgroundStorageKey, JSON.stringify(validated));
  } catch {
    // The in-memory theme remains usable for this visit when storage is unavailable.
  }
}

function withCurrentPaletteCache(record: DailyBackgroundRecord): DailyBackgroundRecord {
  const colorCache = resolveMaterialPaletteCache(
    record.sha256,
    record.sourceColor,
    record.colorCache,
    activeScheme,
  );
  return colorCache === record.colorCache ? record : { ...record, colorCache };
}

function applyPalettes(palettes: { light: MaterialPalette; dark: MaterialPalette }) {
  const root = document.documentElement;
  applyPalette(root, 'light', palettes.light);
  applyPalette(root, 'dark', palettes.dark);
}

function applyPalette(root: HTMLElement, mode: 'light' | 'dark', palette: MaterialPalette) {
  for (const key of materialPaletteKeys) {
    root.style.setProperty(`--ac-m3-${mode}-${toKebabCase(key)}`, palette[key]);
  }
}

function applyBackground({ image, color }: { image?: string; color?: string }) {
  const root = document.documentElement;
  if (image !== undefined) root.style.setProperty('--ac-background-image', image);
  if (color !== undefined) root.style.setProperty('--ac-background-color', color);
  root.dataset.background = 'ready';
}

function releaseUploadedObjectURL() {
  if (!uploadedObjectURL) return;
  URL.revokeObjectURL(uploadedObjectURL);
  uploadedObjectURL = '';
}

function authorizationHeaders() {
  const headers = new Headers();
  const accessKey = getGraphQLAccessKey();
  if (accessKey) headers.set('authorization', `Bearer ${accessKey}`);
  return headers;
}

function attributionFrom(metadata: DailyMetadata) {
  return { title: metadata.title, copyright: metadata.copyright, sourceUrl: attributionSource };
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
