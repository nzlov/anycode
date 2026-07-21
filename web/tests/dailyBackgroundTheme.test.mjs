import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  contrastRatio,
  createMaterialPalettes,
  dailyImageRefreshReason,
  extractSourceColor,
  materialPaletteKeys,
  parseDailyBackgroundRecord,
  parseDailyMetadata,
  resolveMaterialPaletteCache,
  wallpaperColorSchemes,
} from '../src/theme/dailyBackgroundModel.js';

const metadata = {
  sourceDate: '2026/07/20',
  imageUrl: 'https://cn.bing.com/th?id=OHR.Daily_UHD.jpg',
  title: 'Daily image',
  copyright: '© Example',
};

function record(overrides = {}) {
  const sourceColor = '#2563eb';
  return {
    version: 2,
    source: 'ee123',
    localDate: '2026-07-20',
    sourceDate: metadata.sourceDate,
    imageUrl: metadata.imageUrl,
    sha256: 'a'.repeat(64),
    checkedAt: '2026-07-20T10:00:00.000Z',
    loadedAt: '2026-07-20T10:00:01.000Z',
    attribution: {
      title: metadata.title,
      copyright: metadata.copyright,
      sourceUrl: 'https://bing.ee123.net/',
    },
    sourceColor,
    colorCache: {
      sha256: 'a'.repeat(64),
      wallpaperColorScheme: 'content',
      palettes: createMaterialPalettes(sourceColor, 'content'),
    },
    ...overrides,
  };
}

test('metadata accepts only the expected ee123 shape and Bing HTTPS image hosts', () => {
  assert.deepEqual(
    parseDailyMetadata({
      status: '200',
      date: '2026/07/20',
      imgurl: metadata.imageUrl,
      imgtitle: ` ${metadata.title} `,
      imgcopyright: metadata.copyright,
      imgdetail: '<script>ignored</script>',
    }),
    metadata,
  );
  assert.equal(
    parseDailyMetadata({ status: '200', date: '2026/07/20', imgurl: 'http://cn.bing.com/a.jpg' }),
    null,
  );
  assert.equal(
    parseDailyMetadata({
      status: '200',
      date: '2026/07/20',
      imgurl: 'https://example.com/a.jpg',
      imgtitle: 'x',
      imgcopyright: 'y',
    }),
    null,
  );
  assert.equal(
    parseDailyMetadata({
      status: '200',
      date: '2026/02/30',
      imgurl: metadata.imageUrl,
      imgtitle: 'x',
      imgcopyright: 'y',
    }),
    null,
  );
});

test('stored record parsing validates the source and algorithm-keyed palette cache', () => {
  const parsed = parseDailyBackgroundRecord(JSON.stringify({ ...record(), ignored: true }));
  assert.equal(parsed?.imageUrl, metadata.imageUrl);
  assert.equal(parsed?.colorCache.wallpaperColorScheme, 'content');
  assert.equal('ignored' in parsed, false);
  assert.equal(parseDailyBackgroundRecord('{broken'), null);
  assert.equal(parseDailyBackgroundRecord(record({ version: 1 })), null);
  assert.equal(parseDailyBackgroundRecord(record({ sha256: 'short' })), null);
  assert.equal(parseDailyBackgroundRecord(record({ localDate: '2026-02-30' })), null);
  assert.equal(parseDailyBackgroundRecord(record({ checkedAt: '2026' })), null);
  assert.equal(
    parseDailyBackgroundRecord(record({ imageUrl: 'https://evil.example/image.jpg' })),
    null,
  );
  assert.equal(
    parseDailyBackgroundRecord(
      record({ colorCache: { wallpaperColorScheme: 'unknown', palettes: {} } }),
    ),
    null,
  );
  assert.equal(
    parseDailyBackgroundRecord(
      record({ colorCache: { ...record().colorCache, sha256: 'b'.repeat(64) } }),
    ),
    null,
  );
  assert.equal(
    parseDailyBackgroundRecord(
      record({
        colorCache: {
          sha256: 'a'.repeat(64),
          wallpaperColorScheme: 'content',
          palettes: { light: record().colorCache.palettes.light, dark: {} },
        },
      }),
    ),
    null,
  );
});

test('refresh decision checks local day and same-day source replacement', () => {
  const current = record();
  assert.equal(dailyImageRefreshReason(current, metadata, '2026-07-20'), null);
  assert.equal(dailyImageRefreshReason(null, metadata, '2026-07-20'), 'missing-record');
  assert.equal(dailyImageRefreshReason(current, metadata, '2026-07-21'), 'local-date');
  assert.equal(
    dailyImageRefreshReason(current, { ...metadata, sourceDate: '2026/07/21' }, '2026-07-20'),
    'source-date',
  );
  assert.equal(
    dailyImageRefreshReason(
      current,
      { ...metadata, imageUrl: 'https://www.bing.com/new.jpg' },
      '2026-07-20',
    ),
    'image-url',
  );
});

test('official image quantization ignores non-opaque pixels and scores a UI source color', () => {
  const pixels = new Uint8ClampedArray([
    ...Array(20).fill([3, 4, 5, 255]).flat(),
    ...Array(8).fill([25, 110, 210, 255]).flat(),
    ...Array(4).fill([255, 0, 0, 0]).flat(),
  ]);
  assert.equal(extractSourceColor(pixels), '#196ed2');
  assert.equal(extractSourceColor(new Uint8ClampedArray([0, 0, 0, 0])), '#4285f4');
});

test('all official dynamic schemes produce every M3 role including fixed colors', () => {
  assert.deepEqual(wallpaperColorSchemes, [
    'content',
    'fidelity',
    'tonal_spot',
    'vibrant',
    'expressive',
    'rainbow',
    'fruit_salad',
    'neutral',
    'monochrome',
  ]);
  for (const scheme of wallpaperColorSchemes) {
    const palettes = createMaterialPalettes('#2563eb', scheme);
    assert.deepEqual(Object.keys(palettes.light), materialPaletteKeys, scheme);
    assert.deepEqual(Object.keys(palettes.dark), materialPaletteKeys, scheme);
    for (const palette of [palettes.light, palettes.dark]) {
      for (const [foreground, background] of [
        ['onSurface', 'surface'],
        ['onSurfaceVariant', 'surfaceVariant'],
        ['onPrimary', 'primary'],
        ['onPrimaryContainer', 'primaryContainer'],
        ['onSecondary', 'secondary'],
        ['onSecondaryContainer', 'secondaryContainer'],
        ['onTertiary', 'tertiary'],
        ['onTertiaryContainer', 'tertiaryContainer'],
        ['onError', 'error'],
        ['onErrorContainer', 'errorContainer'],
        ['onPrimaryFixed', 'primaryFixed'],
        ['onSecondaryFixed', 'secondaryFixed'],
        ['onTertiaryFixed', 'tertiaryFixed'],
      ]) {
        assert.ok(
          contrastRatio(palette[foreground], palette[background]) >= 4.5,
          `${scheme} ${foreground}`,
        );
      }
    }
  }
});

test('palette cache is reused only when both wallpaper source and algorithm are unchanged', () => {
  const current = {
    sha256: 'a'.repeat(64),
    wallpaperColorScheme: 'rainbow',
    palettes: createMaterialPalettes('#2563eb', 'rainbow'),
  };
  assert.equal(resolveMaterialPaletteCache('a'.repeat(64), '#2563eb', current, 'rainbow'), current);

  const changedAlgorithm = resolveMaterialPaletteCache(
    'a'.repeat(64),
    '#2563eb',
    current,
    'content',
  );
  assert.notEqual(changedAlgorithm, current);
  assert.equal(changedAlgorithm.wallpaperColorScheme, 'content');
  assert.notDeepEqual(changedAlgorithm.palettes, current.palettes);

  const changedWallpaper = resolveMaterialPaletteCache(
    'b'.repeat(64),
    '#e11d48',
    current,
    'rainbow',
  );
  assert.notEqual(changedWallpaper, current);
  assert.equal(changedWallpaper.sha256, 'b'.repeat(64));
  assert.notDeepEqual(changedWallpaper.palettes, current.palettes);
});
