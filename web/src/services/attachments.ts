import { graphqlFetch, graphqlMultipartFetch } from '@/services/graphqlClient';

export interface StagedAttachment {
  id: string;
  filename: string;
  mimeType: string;
  size: number;
  previewable: boolean;
}

const attachmentFields = `
  id
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
