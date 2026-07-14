import { Dark, setCssVar } from 'quasar';

export type ThemeMode = 'system' | 'light' | 'dark';

export const themeStorageKey = 'anycode.theme.mode';

export const themeModes: Array<{ label: string; value: ThemeMode; icon: string }> = [
  { label: '跟随系统', value: 'system', icon: 'devices' },
  { label: '浅色', value: 'light', icon: 'light_mode' },
  { label: '深色', value: 'dark', icon: 'dark_mode' },
];

export const themeTokens = {
  quasar: {
    primary: '#2563eb',
    secondary: '#0f172a',
    accent: '#22c55e',
    positive: '#22c55e',
    negative: '#b91c1c',
    warning: '#f59e0b',
    info: '#0284c7',
  },
  surfaces: {
    light: {
      surface: '#ffffff',
      surfaceMuted: '#f6f8fb',
      surfaceRaised: '#ffffff',
      border: '#d9e0ea',
      text: '#0f172a',
      textMuted: '#64748b',
    },
    dark: {
      surface: '#111827',
      surfaceMuted: '#0f172a',
      surfaceRaised: '#172033',
      border: '#263449',
      text: '#e2e8f0',
      textMuted: '#94a3b8',
    },
  },
};

export function readThemeMode(): ThemeMode {
  const saved = window.localStorage.getItem(themeStorageKey);
  if (saved === 'light' || saved === 'dark' || saved === 'system') {
    return saved;
  }
  return 'system';
}

export function writeThemeMode(mode: ThemeMode) {
  window.localStorage.setItem(themeStorageKey, mode);
  applyThemeMode(mode);
}

export function applyThemeMode(mode: ThemeMode) {
  Dark.set(mode === 'system' ? 'auto' : mode === 'dark');
  document.documentElement.dataset.themeMode = mode;

  Object.entries(themeTokens.quasar).forEach(([name, value]) => {
    setCssVar(name, value);
  });
}
