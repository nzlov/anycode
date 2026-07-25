import { graphqlFetch } from '@/services/graphqlClient';

export interface GeneralSettings {
  agentMaxConcurrent: number;
}

export async function getGeneralSettings() {
  const data = await graphqlFetch<{ generalSettings: GeneralSettings }>({
    query: `
      query GeneralSettings {
        generalSettings { agentMaxConcurrent }
      }
    `,
    notify: false,
  });
  return data.generalSettings;
}

export async function updateGeneralSettings(agentMaxConcurrent: number) {
  const data = await graphqlFetch<
    { updateGeneralSettings: GeneralSettings },
    { input: GeneralSettings }
  >({
    query: `
      mutation UpdateGeneralSettings($input: UpdateGeneralSettingsInput!) {
        updateGeneralSettings(input: $input) { agentMaxConcurrent }
      }
    `,
    variables: { input: { agentMaxConcurrent } },
  });
  return data.updateGeneralSettings;
}
