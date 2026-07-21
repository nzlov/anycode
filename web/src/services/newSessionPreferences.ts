import type { SessionConfig, SessionPriority } from '@/services/sessions';

export interface NewSessionPreferences extends SessionConfig {
  projectId: string;
  baseBranch: string;
  priority: SessionPriority;
}

export const NEW_SESSION_PREFERENCES_STORAGE_KEY = 'anycode.newSessionPreferences';

const legacySessionConfigStorageKey = 'anycode.lastSessionConfig';
const legacyProjectStorageKey = 'anycode.lastNewSessionProjectId';

export function loadNewSessionPreferences(): NewSessionPreferences | null {
  if (typeof window === 'undefined') return null;
  try {
    const stored = parseRecord(window.localStorage.getItem(NEW_SESSION_PREFERENCES_STORAGE_KEY));
    if (stored) return preferencesFromRecord(stored);

    // GLUE: Read the retired keys until an existing browser rewrites them as one preference record.
    const legacyConfig = parseRecord(window.localStorage.getItem(legacySessionConfigStorageKey));
    const legacyProjectId = window.localStorage.getItem(legacyProjectStorageKey) ?? '';
    if (!legacyConfig && !legacyProjectId) return null;
    return preferencesFromRecord({ ...legacyConfig, projectId: legacyProjectId });
  } catch {
    return null;
  }
}

export function hasStoredSessionConfig() {
  const preferences = loadNewSessionPreferences();
  return Boolean(preferences?.codexModel.trim() && preferences.reasoningEffort.trim());
}

export function storeNewSessionPreferences(preferences: NewSessionPreferences) {
  try {
    window.localStorage.setItem(NEW_SESSION_PREFERENCES_STORAGE_KEY, JSON.stringify(preferences));
  } catch {
    // Ignore storage failures; the current page still uses the selected preferences.
  }
}

function parseRecord(raw: string | null): Record<string, unknown> | null {
  if (!raw) return null;
  const parsed = JSON.parse(raw) as unknown;
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return null;
  return parsed as Record<string, unknown>;
}

function preferencesFromRecord(record: Record<string, unknown>): NewSessionPreferences {
  const priority = record.priority;
  return {
    projectId: typeof record.projectId === 'string' ? record.projectId : '',
    baseBranch: typeof record.baseBranch === 'string' ? record.baseBranch : '',
    codexModel: typeof record.codexModel === 'string' ? record.codexModel : '',
    reasoningEffort: typeof record.reasoningEffort === 'string' ? record.reasoningEffort : '',
    permissionMode: typeof record.permissionMode === 'string' ? record.permissionMode : '',
    fastMode: typeof record.fastMode === 'boolean' ? record.fastMode : false,
    priority: priority === 'high' || priority === 'low' ? priority : 'medium',
  };
}
