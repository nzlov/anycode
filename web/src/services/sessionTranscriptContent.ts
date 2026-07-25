import { inject, provide, type InjectionKey } from 'vue';

import {
  getSessionTranscriptEvent,
  type TranscriptEvent,
  type TranscriptItem,
} from '@/services/sessionTimeline';

export type SessionTranscriptEventLoader = (event: TranscriptItem) => Promise<TranscriptEvent>;

const sessionTranscriptEventLoaderKey: InjectionKey<SessionTranscriptEventLoader> = Symbol(
  'session-transcript-event-loader',
);

export function provideSessionTranscriptEventLoader(sessionId: string) {
  provide(sessionTranscriptEventLoaderKey, (event) => {
    if (!event.deferred) return Promise.resolve(event);
    return getSessionTranscriptEvent(
      sessionId,
      event.deferredEventId ?? event.id,
      event.deferred.byteOffset,
    );
  });
}

export function useSessionTranscriptEventLoader() {
  return inject(sessionTranscriptEventLoaderKey, null);
}
