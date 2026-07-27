import { readonly, ref, inject, provide, type InjectionKey, type Ref } from 'vue';

import {
  defaultSendShortcut,
  getGeneralSettings,
  type SendShortcut,
} from '@/services/generalSettings';

interface GeneralSettingsInvalidation {
  revision: Readonly<Ref<number>>;
  sendShortcut: Readonly<Ref<SendShortcut>>;
  invalidate: () => void;
  setSendShortcut: (shortcut: SendShortcut) => void;
}

const generalSettingsInvalidationKey: InjectionKey<GeneralSettingsInvalidation> = Symbol(
  'general-settings-invalidation',
);

export function provideGeneralSettingsInvalidation() {
  const revision = ref(0);
  const sendShortcut = ref<SendShortcut>(defaultSendShortcut);
  let sendShortcutVersion = 0;
  const invalidation: GeneralSettingsInvalidation = {
    revision: readonly(revision),
    sendShortcut: readonly(sendShortcut),
    invalidate: () => {
      revision.value += 1;
    },
    setSendShortcut: (shortcut) => {
      sendShortcutVersion += 1;
      sendShortcut.value = shortcut;
    },
  };
  provide(generalSettingsInvalidationKey, invalidation);
  const loadVersion = sendShortcutVersion;
  void getGeneralSettings()
    .then((settings) => {
      if (loadVersion !== sendShortcutVersion) return;
      sendShortcut.value = settings.sendShortcut;
    })
    .catch(() => undefined);
  return invalidation;
}

export function useGeneralSettingsInvalidation() {
  return inject(generalSettingsInvalidationKey, null);
}
