import assert from 'node:assert/strict';
import { test } from 'node:test';

import { diffMediaKind, diffMediaVersions } from '../src/services/diffMediaModel.js';

test('diff media model recognizes browser-previewable media extensions', () => {
  assert.equal(diffMediaKind('images/Preview.PNG'), 'image');
  assert.equal(diffMediaKind('icons/diagram.svg'), 'image');
  assert.equal(diffMediaKind('images/photo.avif'), 'image');
  assert.equal(diffMediaKind('images/icon.ico'), 'image');
  assert.equal(diffMediaKind('audio/theme.mp3'), 'audio');
  assert.equal(diffMediaKind('audio/voice.opus'), 'audio');
  assert.equal(diffMediaKind('clips/demo.webm'), 'video');
  assert.equal(diffMediaKind('docs/report.pdf'), 'pdf');
  assert.equal(diffMediaKind('models/scene.GLB'), 'model');
  assert.equal(diffMediaKind('models/scene.gltf'), 'model');
  assert.equal(diffMediaKind('models/mesh.obj'), 'model');
  assert.equal(diffMediaKind('models/mesh.stl'), 'model');
  assert.equal(diffMediaKind('models/print.3mf'), 'model');
  assert.equal(diffMediaKind('images/unsupported.tiff'), null);
  assert.equal(diffMediaKind('README.md'), null);
});

test('diff media model selects versions from change status', () => {
  assert.deepEqual(diffMediaVersions('modified'), ['old', 'new']);
  assert.deepEqual(diffMediaVersions('renamed'), ['old', 'new']);
  assert.deepEqual(diffMediaVersions('added'), ['new']);
  assert.deepEqual(diffMediaVersions('deleted'), ['old']);
});
