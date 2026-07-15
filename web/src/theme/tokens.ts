import { Dark } from 'quasar';

export type ThemeMode = 'system' | 'light' | 'dark';

export const themeStorageKey = 'anycode.theme.mode';

export const themeModes: Array<{ label: string; value: ThemeMode; icon: string }> = [
  { label: '跟随系统', value: 'system', icon: 'devices' },
  { label: '浅色', value: 'light', icon: 'light_mode' },
  { label: '深色', value: 'dark', icon: 'dark_mode' },
];

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
}
