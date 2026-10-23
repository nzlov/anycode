import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
import { test } from 'node:test';
import { runInNewContext } from 'node:vm';
import { browserPreviewKind } from '../src/services/browserPreviewModel.js';

const source = stripTypeScriptTypes(
  readFileSync(new URL('../src/services/attachments.ts', import.meta.url), 'utf8'),
)
  .replace(/^import .*;$/gm, '')
  .replaceAll('export ', '');
const preview = runInNewContext(`${source}\nattachmentPreviewFile;`, { browserPreviewKind });

for (const [filename, mimeType, previewKind] of [
  ['photo.png', 'image/png', 'image'],
  ['clip.mp4', 'video/mp4', 'video'],
  ['sound.mp3', 'audio/mpeg', 'audio'],
  ['paper.pdf', 'application/pdf', 'pdf'],
  ['shape.glb', 'model/gltf-binary', 'model'],
  ['notes.md', 'text/markdown', 'text'],
]) {
  test(`uploaded ${filename} uses authenticated file preview`, () => {
    const file = preview({ id: 'upload #1', filename, mimeType, size: 42, previewable: true });
    assert.equal(file.previewKind, previewKind);
    assert.equal(file.previewUrl, '/files/upload%20%231/preview');
    assert.equal(file.downloadUrl, '/files/upload%20%231/download');
    assert.equal(file.size, 42);
  });
}

test('non-previewable uploads retain download access', () => {
  const file = preview({
    id: 'archive',
    filename: 'archive.zip',
    mimeType: 'application/zip',
    previewable: false,
  });
  assert.equal(file.previewKind, 'none');
  assert.equal(file.previewUrl, null);
  assert.equal(file.downloadUrl, '/files/archive/download');
});
