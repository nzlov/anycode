import { type CodexModelOption } from '@/components/promptOptions';
import { graphqlFetch } from '@/services/graphqlClient';

interface CodexModelOptionsResponse {
  codexModelOptions: CodexModelOption[];
}

let codexModelOptionsRequest: Promise<CodexModelOption[]> | null = null;

export function listCodexModelOptions() {
  if (!codexModelOptionsRequest) {
    codexModelOptionsRequest = graphqlFetch<CodexModelOptionsResponse>({
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
    })
      .then((data) => data.codexModelOptions)
      .catch((error: unknown) => {
        codexModelOptionsRequest = null;
        throw error;
      });
  }
  return codexModelOptionsRequest;
}
