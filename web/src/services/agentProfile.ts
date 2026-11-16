import { graphqlFetch } from '@/services/graphqlClient';

export interface ProfileSchedule {
  valid: boolean;
  description: string;
  error: string;
  timezone: string;
  nextRunAt: string | null;
}

export interface ProfileConfiguration {
  enabled: boolean;
  cron: string;
  model: string;
  reasoningEffort: string;
}

export interface AgentProfile extends ProfileConfiguration {
  prompt: string;
  status: string;
  error: string;
  through: string | null;
  startedAt: string | null;
  finishedAt: string | null;
  sessionCount: number;
  messageCount: number;
  progress: {
    stage: string;
    percent: number;
    completedMessages: number;
    totalMessages: number;
  };
  schedule: ProfileSchedule;
}

const scheduleFields = 'valid description error timezone nextRunAt';
const profileFields = `enabled cron model reasoningEffort prompt status error through startedAt finishedAt sessionCount messageCount progress { stage percent completedMessages totalMessages } schedule { ${scheduleFields} }`;

export async function getAgentProfile() {
  const data = await graphqlFetch<{ agentProfile: AgentProfile }>({
    query: `query AgentProfile { agentProfile { ${profileFields} } }`,
    notify: false,
  });
  return data.agentProfile;
}

export async function previewProfileSchedule(cron: string) {
  const data = await graphqlFetch<{ agentProfileSchedule: ProfileSchedule }, { cron: string }>({
    query: `query ProfileSchedule($cron: String!) { agentProfileSchedule(cron: $cron) { ${scheduleFields} } }`,
    variables: { cron },
    notify: false,
  });
  return data.agentProfileSchedule;
}

export async function configureAgentProfile(input: ProfileConfiguration) {
  const data = await graphqlFetch<
    { configureAgentProfile: AgentProfile },
    { input: ProfileConfiguration }
  >({
    query: `mutation ConfigureAgentProfile($input: AgentProfileConfigurationInput!) { configureAgentProfile(input: $input) { ${profileFields} } }`,
    variables: { input },
    notify: false,
  });
  return data.configureAgentProfile;
}

export async function analyzeAgentProfile() {
  const data = await graphqlFetch<{ analyzeAgentProfile: AgentProfile }>({
    query: `mutation AnalyzeAgentProfile { analyzeAgentProfile { ${profileFields} } }`,
    notify: false,
  });
  return data.analyzeAgentProfile;
}
