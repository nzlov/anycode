import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

import { formatPreviewAnnotationDraft } from '../src/services/previewAnnotations.ts';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const annotator = readSource('../src/components/PreviewAnnotator.vue');
const diffViewer = readSource('../src/components/DiffViewer.vue');
const diffMedia = readSource('../src/components/DiffMediaPreview.vue');
const filePreview = readSource('../src/components/SessionFilePreview.vue');
const detail = readSource('../src/components/SessionDetailView.vue');
const layout = readSource('../src/layouts/MainLayout.vue');
const composer = readSource('../src/components/PromptComposer.vue');
const attachments = readSource('../src/services/attachments.ts');
const sessions = readSource('../src/services/sessions.ts');

test('annotation draft contains marked file, source ranges, selected text, and notes', () => {
  const draft = formatPreviewAnnotationDraft('变更 web/src/App.vue', [
    {
      id: 'image-1',
      kind: 'image',
      shape: 'rectangle',
      x: 0.125,
      y: 0.25,
      width: 0.5,
      height: 0.375,
      note: '按钮应右对齐',
    },
    {
      id: 'text-1',
      kind: 'text',
      start: { line: 8, column: 3, revision: 'new' },
      end: { line: 8, column: 23, revision: 'new' },
      quote: 'const active = true;',
      note: '请改成派生状态',
    },
  ]);

  assert.match(draft, /^文件标注\n标记文件：变更 web\/src\/App\.vue/m);
  assert.match(
    draft,
    /框选范围：矩形；左上 \(12\.5%, 25\.0%\)；右下 \(62\.5%, 62\.5%\)；宽 50\.0%，高 37\.5%/,
  );
  assert.match(draft, /文本范围：新文件第 8 行第 3 列 至 新文件第 8 行第 23 列（结束位置不含）/);
  assert.match(draft, /> const active = true;/);
  assert.match(draft, /标注：按钮应右对齐/);
  assert.match(draft, /标注：请改成派生状态/);
});

test('image-only annotation draft identifies the marked image and normalized bounds', () => {
  const draft = formatPreviewAnnotationDraft('临时文件 design.png', [
    {
      id: 'image-1',
      kind: 'image',
      shape: 'ellipse',
      x: 0.1,
      y: 0.2,
      width: 0.3,
      height: 0.4,
      note: '移除水印',
    },
  ]);

  assert.match(draft, /^图片标注\n标记文件：临时文件 design\.png/m);
  assert.match(draft, /框选范围：圆形；左上 \(10\.0%, 20\.0%\)；右下 \(40\.0%, 60\.0%\)/);
  assert.match(draft, /标注：移除水印/);
});

test('image previews expose a split rectangle and ellipse selector with resizable notes', () => {
  assert.match(annotator, /<q-btn-dropdown[\s\S]*split[\s\S]*label="框选"/);
  assert.match(annotator, /armShape\('rectangle'\)/);
  assert.match(annotator, /armShape\('ellipse'\)/);
  assert.match(annotator, /startResize\(\$event, annotation, corner\)/);
  assert.match(annotator, /label="备注（可选）"/);
  assert.match(diffMedia, /<PreviewAnnotator[\s\S]*mode="image"/);
  assert.match(filePreview, /<PreviewAnnotator[\s\S]*mode="image"/);
});

test('text files and code diffs use selected-text highlights and comments', () => {
  assert.match(annotator, /range\.cloneRange\(\)/);
  assert.match(annotator, /range\.getClientRects\(\)/);
  assert.match(annotator, /label="批注选中内容"/);
  assert.match(diffViewer, /<PreviewAnnotator[\s\S]*mode="text"/);
  assert.match(filePreview, /<PreviewAnnotator[\s\S]*mode="text"/);
  assert.match(filePreview, /data-annotation-text data-annotation-line="1"/);
  assert.match(
    diffViewer,
    /:data-annotation-line="line\.newLine \?\? line\.oldLine \?\? undefined"/,
  );
  assert.match(annotator, /sourceTextRange\(range\)/);
});

test('touch text selections survive tapping the mobile annotation toolbar', () => {
  assert.match(
    annotator,
    /document\.addEventListener\('selectionchange', captureTextSelection\)/,
  );
  assert.match(
    annotator,
    /document\.removeEventListener\('selectionchange', captureTextSelection\)/,
  );
  assert.match(annotator, /if \(selection\.isCollapsed\) return;/);
});

test('multi-line comments include only annotation text and exclude diff line numbers', () => {
  assert.match(annotator, /editorQuote\.value = selectedAnnotationText\(range\)/);
  assert.match(
    annotator,
    /querySelectorAll<HTMLElement>\('\[data-annotation-text\]'\)[\s\S]*range\.intersectsNode\(textRoot\)/,
  );
  assert.match(annotator, /\.map\(\(textRoot\) => \{[\s\S]*selected\.selectNodeContents\(textRoot\)/);
  assert.match(annotator, /\.join\('\\n'\)/);
  assert.doesNotMatch(annotator, /editorQuote\.value = range\.toString\(\)/);
});

test('inject adds every new annotation as a typed composer attachment then clears it', () => {
  assert.match(annotator, /formatPreviewAnnotationDraft\(props\.source, annotations\.value\)/);
  assert.match(annotator, /injector\.inject[\s\S]*clearAnnotations\(\)/);
  assert.match(detail, /appendAnnotations\.value = \[\.\.\.appendAnnotations\.value, attachment\]/);
  assert.match(detail, /await stageAnnotation\(annotation\)/);
  assert.match(detail, /composerCollapsed\.value = false/);
  assert.match(layout, /state: \{ annotationAttachment: JSON\.stringify\(attachment\) \}/);
  assert.match(detail, /consumeNavigationAnnotationAttachment\(\)/);
  assert.match(
    composer,
    /v-for="annotation in annotations"[\s\S]*批注 · \{\{ annotation\.source[\s\S]*含原文件/,
  );
  assert.match(attachments, /mutation StageAnnotation\(\$input: StageAnnotationInput!\)/);
  assert.match(detail, /attachment\.kind === 'annotation' \? '批注' : '上传'/);
});

test('annotation source files use generic zero-copy references across file kinds', () => {
  assert.match(sessions, /export interface PromptFileReference/);
  assert.match(sessions, /kind: 'session_file' \| 'diff'/);
  assert.match(sessions, /fileReferences\?: PromptFileReference\[\]/);
  assert.doesNotMatch(sessions, /imageReferences/);
  assert.match(annotator, /fileReferences\?: PreviewFileReference\[\]/);
  assert.match(annotator, /\.\.\.\(fileReferences\.length > 0 \? \{ fileReferences \} : \{\}\)/);
  assert.match(filePreview, /kind: 'session_file', sessionFileId: file\.id/);
  assert.match(diffMedia, /kind: 'diff', filePath, version/);
  assert.match(diffViewer, /kind: 'diff', filePath: file\.path, version: 'old'/);
  assert.match(diffViewer, /kind: 'diff', filePath: file\.path, version: 'new'/);
  assert.match(
    detail,
    /selectedAnnotations\.flatMap\(\(annotation\) => annotation\.fileReferences \?\? \[\]\)/,
  );
});

test('one-time annotations reset when the preview content changes', () => {
  assert.match(annotator, /watch\(\(\) => props\.contentKey, clearAnnotations\)/);
  assert.match(filePreview, /:content-key="file\.id"/);
  assert.match(diffViewer, /:content-key="fileDiffFor\(file\.path\)"/);
  assert.match(diffMedia, /:content-key="states\[version\]\.url"/);
});
