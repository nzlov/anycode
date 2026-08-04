import assert from 'node:assert/strict';
import { test } from 'node:test';

import { browserPreviewKind } from '../src/services/browserPreviewModel.js';

test('browser preview model keeps one explicit format contract', () => {
  assert.equal(browserPreviewKind('diagram.svg'), 'image');
  assert.equal(browserPreviewKind('photo', 'image/avif'), 'image');
  assert.equal(browserPreviewKind('sound.opus'), 'audio');
  assert.equal(browserPreviewKind('clip.mpeg'), 'video');
  assert.equal(browserPreviewKind('report.pdf'), 'pdf');
  assert.equal(browserPreviewKind('scene.glb'), 'model');
  assert.equal(browserPreviewKind('scan.tiff', 'image/tiff'), null);
  assert.equal(browserPreviewKind('photo.heic', 'image/heic'), null);
});
