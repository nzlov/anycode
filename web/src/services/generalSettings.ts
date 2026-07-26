import { graphqlFetch } from '@/services/graphqlClient';

export interface GeneralSettings {
  agentMaxConcurrent: number;
  agentWritableRoots: string[];
  mindMapEnabled: boolean;
  mindMapMode: 'realtime' | 'async';
  mindMapModel: string;
  mindMapReasoningEffort: string;
  mindMapMaxConcurrent: number;
}

const generalSettingsFields = `
  agentMaxConcurrent
  agentWritableRoots
  mindMapEnabled
  mindMapMode
  mindMapModel
  mindMapReasoningEffort
  mindMapMaxConcurrent
`;

export async function getGeneralSettings() {
  const data = await graphqlFetch<{ generalSettings: GeneralSettings }>({
    query: `
      query GeneralSettings {
        generalSettings { ${generalSettingsFields} }
      }
    `,
    notify: false,
  });
  return data.generalSettings;
}

export async function updateGeneralSettings(settings: GeneralSettings) {
  const data = await graphqlFetch<
    { updateGeneralSettings: GeneralSettings },
    { input: GeneralSettings }
  >({
    query: `
      mutation UpdateGeneralSettings($input: UpdateGeneralSettingsInput!) {
        updateGeneralSettings(input: $input) { ${generalSettingsFields} }
      }
    `,
    variables: { input: settings },
  });
  return data.updateGeneralSettings;
}
