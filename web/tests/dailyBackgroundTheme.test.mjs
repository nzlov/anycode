import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  contrastRatio,
  createDailyPalettes,
  dailyImageRefreshReason,
  dailyPaletteKeys,
  extractSeedColors,
  parseDailyBackgroundRecord,
  parseDailyMetadata,
} from '../src/theme/dailyBackgroundModel.js';

const metadata = {
  sourceDate: '2026/07/20',
  imageUrl: 'https://cn.bing.com/th?id=OHR.Daily_UHD.jpg',
  title: 'Daily image',
  copyright: '© Example',
};

function record(overrides = {}) {
  const seed = { dominant: '#38506a', accent: '#2563eb' };
  return {
    version: 1,
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
    seed,
    palettes: createDailyPalettes(seed),
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

test('stored record parsing is all-or-nothing and strips unknown fields', () => {
  const parsed = parseDailyBackgroundRecord(JSON.stringify({ ...record(), ignored: true }));
  assert.equal(parsed?.imageUrl, metadata.imageUrl);
  assert.equal('ignored' in parsed, false);
  assert.equal(parseDailyBackgroundRecord('{broken'), null);
  assert.equal(parseDailyBackgroundRecord(record({ version: 2 })), null);
  assert.equal(parseDailyBackgroundRecord(record({ sha256: 'short' })), null);
  assert.equal(parseDailyBackgroundRecord(record({ localDate: '2026-02-30' })), null);
  assert.equal(parseDailyBackgroundRecord(record({ checkedAt: '2026' })), null);
  assert.equal(
    parseDailyBackgroundRecord(record({ imageUrl: 'https://evil.example/image.jpg' })),
    null,
  );
  assert.equal(
    parseDailyBackgroundRecord(record({ palettes: { light: record().palettes.light, dark: {} } })),
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

test('quantization ignores transparent pixels and prefers a chromatic accent over black', () => {
  const pixels = new Uint8ClampedArray([
    ...Array(20).fill([3, 4, 5, 255]).flat(),
    ...Array(8).fill([25, 110, 210, 255]).flat(),
    ...Array(4).fill([255, 0, 0, 0]).flat(),
  ]);
  assert.deepEqual(extractSeedColors(pixels), { dominant: '#030405', accent: '#196ed2' });
  assert.deepEqual(extractSeedColors(new Uint8ClampedArray([0, 0, 0, 0])), {
    dominant: '#2563eb',
    accent: '#2563eb',
  });
});

test('a large low-saturation region does not suppress a smaller chromatic accent', () => {
  const pixels = new Uint8ClampedArray([
    ...Array(100).fill([128, 128, 128, 255]).flat(),
    ...Array(8).fill([25, 110, 210, 255]).flat(),
  ]);
  assert.deepEqual(extractSeedColors(pixels), { dominant: '#808080', accent: '#196ed2' });
});

test('derived light and dark palettes are symmetric and meet contrast contracts', () => {
  for (const accent of ['#2563eb', '#facc15', '#e11d48', '#111111', '#ffffff']) {
    const palettes = createDailyPalettes({ dominant: accent, accent });
    assert.deepEqual(Object.keys(palettes.light), dailyPaletteKeys);
    assert.deepEqual(Object.keys(palettes.dark), dailyPaletteKeys);
    for (const palette of [palettes.light, palettes.dark]) {
      assert.ok(contrastRatio(palette.text, palette.surface) >= 4.5, accent);
      assert.ok(contrastRatio(palette.textMuted, palette.surface) >= 4.5, accent);
      assert.ok(contrastRatio(palette.link, palette.surface) >= 4.5, accent);
      assert.ok(contrastRatio(palette.onPrimary, palette.primary) >= 4.5, accent);
      assert.ok(contrastRatio(palette.focusRing, palette.surface) >= 3, accent);
      assert.ok(contrastRatio(palette.borderStrong, palette.surface) >= 3, accent);
    }
  }
});
