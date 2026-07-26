import { graphqlFetch } from '@/services/graphqlClient';

export interface QuickCommand {
  id: string;
  projectId?: string | null;
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
  projectId
  content
  createdAt
`;

export async function listQuickCommands(input: {
  projectId?: string;
  includeGlobal?: boolean;
  page: number;
  pageSize: number;
}) {
  const data = await graphqlFetch<
    { quickCommands: QuickCommandPage },
    {
      input: {
        projectId?: string;
        includeGlobal?: boolean;
        page: number;
        pageSize: number;
      };
    }
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

export async function createQuickCommand(content: string, projectId?: string) {
  const data = await graphqlFetch<
    { createQuickCommand: QuickCommand },
    { input: { projectId?: string; content: string } }
  >({
    query: `
      mutation CreateQuickCommand($input: CreateQuickCommandInput!) {
        createQuickCommand(input: $input) {
          ${quickCommandFields}
        }
      }
    `,
    variables: { input: { ...(projectId ? { projectId } : {}), content } },
  });
  return data.createQuickCommand;
}

export async function updateQuickCommand(id: string, content: string) {
  const data = await graphqlFetch<
    { updateQuickCommand: QuickCommand },
    { input: { id: string; content: string } }
  >({
    query: `
      mutation UpdateQuickCommand($input: UpdateQuickCommandInput!) {
        updateQuickCommand(input: $input) {
          ${quickCommandFields}
        }
      }
    `,
    variables: { input: { id, content } },
  });
  return data.updateQuickCommand;
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
