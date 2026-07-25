import { graphqlFetch } from '@/services/graphqlClient';

export interface GeneralSettings {
  agentMaxConcurrent: number;
  agentWritableRoots: string[];
}

export async function getGeneralSettings() {
  const data = await graphqlFetch<{ generalSettings: GeneralSettings }>({
    query: `
      query GeneralSettings {
        generalSettings { agentMaxConcurrent agentWritableRoots }
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
        updateGeneralSettings(input: $input) { agentMaxConcurrent agentWritableRoots }
      }
    `,
    variables: { input: settings },
  });
  return data.updateGeneralSettings;
}
