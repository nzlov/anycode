import { graphqlFetch } from '@/services/graphqlClient';

export interface CodexSettings {
  contextWindow: number | null;
  autoCompactTokenLimit: number | null;
  agentMaxConcurrent: number;
}

const codexSettingsFields = `
  contextWindow
  autoCompactTokenLimit
  agentMaxConcurrent
`;

export async function getCodexSettings() {
  const data = await graphqlFetch<{ codexSettings: CodexSettings }>({
    query: `
      query CodexSettings {
        codexSettings { ${codexSettingsFields} }
      }
    `,
    notify: false,
  });
  return data.codexSettings;
}

export async function updateCodexSettings(settings: CodexSettings) {
  const data = await graphqlFetch<{ updateCodexSettings: CodexSettings }, { input: CodexSettings }>(
    {
      query: `
      mutation UpdateCodexSettings($input: UpdateCodexSettingsInput!) {
        updateCodexSettings(input: $input) { ${codexSettingsFields} }
      }
    `,
      variables: { input: settings },
    },
  );
  return data.updateCodexSettings;
}
