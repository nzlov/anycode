import { readonly, ref, inject, provide, type InjectionKey, type Ref } from 'vue';

interface GeneralSettingsInvalidation {
  revision: Readonly<Ref<number>>;
  invalidate: () => void;
}

const generalSettingsInvalidationKey: InjectionKey<GeneralSettingsInvalidation> = Symbol(
  'general-settings-invalidation',
);

export function provideGeneralSettingsInvalidation() {
  const revision = ref(0);
  const invalidation: GeneralSettingsInvalidation = {
    revision: readonly(revision),
    invalidate: () => {
      revision.value += 1;
    },
  };
  provide(generalSettingsInvalidationKey, invalidation);
  return invalidation;
}

export function useGeneralSettingsInvalidation() {
  return inject(generalSettingsInvalidationKey, null);
}
