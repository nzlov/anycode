import { inject, provide, type InjectionKey } from 'vue';

export {
  matchChangedFilePath,
  parseSessionEventResourceReference,
} from './sessionEventResourceReference';
export type { SessionEventResourceReference } from './sessionEventResourceReference';

export type SessionEventResourceOpener = (reference: string, label?: string) => boolean;

const sessionEventResourceOpenerKey: InjectionKey<SessionEventResourceOpener> = Symbol(
  'session-event-resource-opener',
);

export function provideSessionEventResourceOpener(opener: SessionEventResourceOpener) {
  provide(sessionEventResourceOpenerKey, opener);
}

export function useSessionEventResourceOpener() {
  return inject(sessionEventResourceOpenerKey, null);
}
