import type { PromptFileReference } from '@/services/sessions';

export type ImageAnnotationShape = 'rectangle' | 'ellipse';

export interface ImagePreviewAnnotation {
  id: string;
  kind: 'image';
  shape: ImageAnnotationShape;
  x: number;
  y: number;
  width: number;
  height: number;
  note: string;
}

export type TextAnnotationRevision = 'old' | 'new';

export interface TextAnnotationPosition {
  line: number;
  column: number;
  revision?: TextAnnotationRevision;
}

export interface TextPreviewAnnotation {
  id: string;
  kind: 'text';
  start: TextAnnotationPosition;
  end: TextAnnotationPosition;
  quote: string;
  note: string;
}

export type PreviewAnnotation = ImagePreviewAnnotation | TextPreviewAnnotation;

export type PreviewFileReference = PromptFileReference;

export interface PreviewAnnotationAttachment {
  id: string;
  source: string;
  content: string;
  marks: PreviewAnnotation[];
  fileReferences?: PreviewFileReference[];
}

export function supportsPreviewAnnotations(previewKind: string | null | undefined) {
  return previewKind === 'image' || previewKind === 'text';
}

export function formatPreviewAnnotationDraft(source: string, annotations: PreviewAnnotation[]) {
  const lines = [
    annotations.every((annotation) => annotation.kind === 'image') ? '图片标注' : '文件标注',
    `标记文件：${source.trim() || '当前内容'}`,
  ];
  annotations.forEach((annotation, index) => {
    if (annotation.kind === 'image') {
      const shape = annotation.shape === 'ellipse' ? '圆形' : '矩形';
      lines.push(
        `${index + 1}. 框选范围：${shape}；左上 (${percent(annotation.x)}, ${percent(annotation.y)})；右下 (${percent(annotation.x + annotation.width)}, ${percent(annotation.y + annotation.height)})；宽 ${percent(annotation.width)}，高 ${percent(annotation.height)}`,
      );
    } else {
      lines.push(
        `${index + 1}. 文本范围：${formatTextPosition(annotation.start)} 至 ${formatTextPosition(annotation.end)}（结束位置不含）`,
        '   选中文字：',
      );
      for (const quoteLine of annotation.quote.trim().split('\n')) {
        lines.push(`   > ${quoteLine}`);
      }
    }
    if (annotation.note.trim()) lines.push(`   标注：${annotation.note.trim()}`);
  });
  return lines.join('\n');
}

function formatTextPosition(position: TextAnnotationPosition) {
  const revision =
    position.revision === 'old' ? '旧文件' : position.revision === 'new' ? '新文件' : '';
  return `${revision}第 ${position.line} 行第 ${position.column} 列`;
}

function percent(value: number) {
  return `${(Math.max(0, Math.min(1, value)) * 100).toFixed(1)}%`;
}
