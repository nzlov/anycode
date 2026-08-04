import { graphqlFetch, graphqlMultipartFetch } from '@/services/graphqlClient';
import type { PreviewAnnotationAttachment } from '@/services/previewAnnotations';

export interface StagedAttachment {
  id: string;
  kind: 'upload' | 'annotation';
  filename: string;
  mimeType: string;
  size: number;
  previewable: boolean;
}

const attachmentFields = `
  id
  kind
  filename
  mimeType
  size
  previewable
`;

export async function stageAttachment(file: File) {
  const body = new FormData();
  body.append(
    'operations',
    JSON.stringify({
      query: `
        mutation StageAttachment($file: Upload!) {
          stageAttachment(file: $file) {
            ${attachmentFields}
          }
        }
      `,
      variables: { file: null },
    }),
  );
  body.append('map', JSON.stringify({ '0': ['variables.file'] }));
  body.append('0', file, file.name);

  const data = await graphqlMultipartFetch<{ stageAttachment: StagedAttachment }>(body);
  return data.stageAttachment;
}

export async function stageAnnotation(annotation: PreviewAnnotationAttachment) {
  const filename = `批注-${annotation.source.replaceAll('/', '-').slice(0, 80) || '当前内容'}.md`;
  const data = await graphqlFetch<
    { stageAnnotation: StagedAttachment },
    { input: { filename: string; content: string } }
  >({
    query: `
      mutation StageAnnotation($input: StageAnnotationInput!) {
        stageAnnotation(input: $input) {
          ${attachmentFields}
        }
      }
    `,
    variables: { input: { filename, content: annotation.content } },
  });
  return data.stageAnnotation;
}

export async function deleteStagedAttachment(id: string, options: { notify?: boolean } = {}) {
  const request: {
    query: string;
    variables: { id: string };
    notify?: boolean;
  } = {
    query: `
      mutation DeleteStagedAttachment($id: ID!) {
        deleteStagedAttachment(id: $id)
      }
    `,
    variables: { id },
  };
  if (options.notify !== undefined) {
    request.notify = options.notify;
  }
  const data = await graphqlFetch<{ deleteStagedAttachment: boolean }, { id: string }>(request);
  return data.deleteStagedAttachment;
}
