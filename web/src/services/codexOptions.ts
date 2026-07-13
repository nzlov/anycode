import { type CodexModelOption } from '@/components/promptOptions';
import { graphqlFetch } from '@/services/graphqlClient';

interface CodexModelOptionsResponse {
  codexModelOptions: CodexModelOption[];
}

export async function listCodexModelOptions() {
  const data = await graphqlFetch<CodexModelOptionsResponse>({
    query: `
      query CodexModelOptions {
        codexModelOptions {
          label
          value
          defaultReasoningEffort
          reasoningEfforts {
            label
            value
            description
          }
        }
      }
    `,
  });
  return data.codexModelOptions;
}
