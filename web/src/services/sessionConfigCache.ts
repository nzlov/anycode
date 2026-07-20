import type { SessionConfig } from '@/services/sessions';

export const SESSION_CONFIG_STORAGE_KEY = 'anycode.lastSessionConfig';

export function loadStoredSessionConfig(): SessionConfig | null {
  if (typeof window === 'undefined') return null;
  try {
    const raw = window.localStorage.getItem(SESSION_CONFIG_STORAGE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as unknown;
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return null;
    const config = parsed as Record<string, unknown>;
    return {
      codexModel: typeof config.codexModel === 'string' ? config.codexModel : '',
      reasoningEffort: typeof config.reasoningEffort === 'string' ? config.reasoningEffort : '',
      permissionMode: typeof config.permissionMode === 'string' ? config.permissionMode : '',
      fastMode: typeof config.fastMode === 'boolean' ? config.fastMode : false,
    };
  } catch {
    return null;
  }
}

export function hasStoredSessionConfig() {
  const config = loadStoredSessionConfig();
  return Boolean(config?.codexModel.trim() && config.reasoningEffort.trim());
}

export function storeSessionConfig(config: SessionConfig) {
  try {
    window.localStorage.setItem(SESSION_CONFIG_STORAGE_KEY, JSON.stringify(config));
  } catch {
    // Ignore storage failures; the current session still uses the selected config.
  }
}
