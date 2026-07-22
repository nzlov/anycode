import { graphqlFetch } from '@/services/graphqlClient';
import type { PromptSlashCommand } from '@/services/promptCompletionText.js';

export type { PromptSlashCommand } from '@/services/promptCompletionText.js';

export interface PromptFileMatch {
  path: string;
  score: number;
  indices: number[];
}

export interface PromptFileSearchInput {
  projectId?: string;
  sessionId?: string;
  query: string;
}

let slashCommandsRequest: Promise<PromptSlashCommand[]> | null = null;

export function listCodexSlashCommands() {
  if (!slashCommandsRequest) {
    slashCommandsRequest = graphqlFetch<{ codexSlashCommands: PromptSlashCommand[] }>({
      query: `
        query CodexSlashCommands {
          codexSlashCommands {
            name
            description
            acceptsArgs
            requiresThread
          }
        }
      `,
      notify: false,
    })
      .then((data) => data.codexSlashCommands)
      .catch((error: unknown) => {
        slashCommandsRequest = null;
        throw error;
      });
  }
  return slashCommandsRequest;
}

export async function searchPromptFiles(input: PromptFileSearchInput) {
  const data = await graphqlFetch<
    { promptFileMatches: PromptFileMatch[] },
    { input: PromptFileSearchInput }
  >({
    query: `
      query PromptFileMatches($input: PromptFileMatchInput!) {
        promptFileMatches(input: $input) {
          path
          score
          indices
        }
      }
    `,
    variables: { input },
    notify: false,
  });
  return data.promptFileMatches;
}
