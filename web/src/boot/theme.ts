import { defineBoot } from '#q-app';
import { Dark, setCssVar } from 'quasar';

type ThemeMode = 'system' | 'light' | 'dark';

const storageKey = 'anycode.theme.mode';
const defaultMode: ThemeMode = 'system';

const themeTokens = {
  primary: '#2563eb',
  secondary: '#0f766e',
  accent: '#22c55e',
};

function readThemeMode(): ThemeMode {
  const saved = window.localStorage.getItem(storageKey);
  if (saved === 'light' || saved === 'dark' || saved === 'system') {
    return saved;
  }
  return defaultMode;
}

function applyTheme(mode: ThemeMode) {
  Dark.set(mode === 'system' ? 'auto' : mode === 'dark');
  document.documentElement.dataset.themeMode = mode;
  setCssVar('primary', themeTokens.primary);
  setCssVar('secondary', themeTokens.secondary);
  setCssVar('accent', themeTokens.accent);
}

export default defineBoot(() => {
  applyTheme(readThemeMode());
});
