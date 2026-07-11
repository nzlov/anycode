import { graphqlFetch } from '@/services/graphqlClient';

export interface QuickCommand {
  id: string;
  content: string;
  createdAt: string;
}

export interface QuickCommandPage {
  items: QuickCommand[];
  pageInfo: {
    page: number;
    pageSize: number;
    total: number;
    nextCursor: string;
  };
}

const quickCommandFields = `
  id
  content
  createdAt
`;

export async function listQuickCommands(input: { page: number; pageSize: number }) {
  const data = await graphqlFetch<
    { quickCommands: QuickCommandPage },
    { input: { page: number; pageSize: number } }
  >({
    query: `
      query QuickCommands($input: ListQuickCommandsInput!) {
        quickCommands(input: $input) {
          items {
            ${quickCommandFields}
          }
          pageInfo {
            page
            pageSize
            total
            nextCursor
          }
        }
      }
    `,
    variables: { input },
  });
  return data.quickCommands;
}

export async function createQuickCommand(content: string) {
  const data = await graphqlFetch<
    { createQuickCommand: QuickCommand },
    { input: { content: string } }
  >({
    query: `
      mutation CreateQuickCommand($input: CreateQuickCommandInput!) {
        createQuickCommand(input: $input) {
          ${quickCommandFields}
        }
      }
    `,
    variables: { input: { content } },
  });
  return data.createQuickCommand;
}

export async function deleteQuickCommand(id: string) {
  const data = await graphqlFetch<{ deleteQuickCommand: boolean }, { id: string }>({
    query: `
      mutation DeleteQuickCommand($id: ID!) {
        deleteQuickCommand(id: $id)
      }
    `,
    variables: { id },
  });
  return data.deleteQuickCommand;
}
