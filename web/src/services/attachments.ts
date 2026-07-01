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

export async function deleteStagedAttachment(id: string) {
  const data = await graphqlFetch<{ deleteStagedAttachment: boolean }, { id: string }>({
    query: `
      mutation DeleteStagedAttachment($id: ID!) {
        deleteStagedAttachment(id: $id)
      }
    `,
    variables: { id },
  });
  return data.deleteStagedAttachment;
}
