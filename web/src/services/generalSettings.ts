import { graphqlFetch } from '@/services/graphqlClient';

export interface GeneralSettings {
  agentMaxConcurrent: number;
  agentWritableRoots: string[];
  sendShortcut: SendShortcut;
  mindMapEnabled: boolean;
  mindMapMode: 'realtime' | 'async';
  mindMapLayout: MindMapLayout;
  mindMapModel: string;
  mindMapReasoningEffort: string;
  mindMapMaxConcurrent: number;
}

export type SendShortcut = 'enter' | 'shift_enter';
export type MindMapLayout = 'radial' | 'nested';

export const mindMapLayoutOptions: { label: string; value: MindMapLayout }[] = [
  { label: '同心放射', value: 'radial' },
  { label: '关联分层', value: 'nested' },
];

export const defaultSendShortcut: SendShortcut = 'shift_enter';

const generalSettingsFields = `
  agentMaxConcurrent
  agentWritableRoots
  sendShortcut
  mindMapEnabled
  mindMapMode
  mindMapLayout
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
