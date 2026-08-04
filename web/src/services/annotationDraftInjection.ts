import { inject, provide, type InjectionKey } from 'vue';

import type { PreviewAnnotationAttachment } from '@/services/previewAnnotations';

export interface AnnotationDraftInjector {
  canInject: (sessionId?: string) => boolean;
  inject: (attachment: PreviewAnnotationAttachment, sessionId?: string) => void;
}

const annotationDraftInjectorKey: InjectionKey<AnnotationDraftInjector> = Symbol(
  'annotation-draft-injector',
);

export function provideAnnotationDraftInjector(injector: AnnotationDraftInjector) {
  provide(annotationDraftInjectorKey, injector);
}

export function useAnnotationDraftInjector() {
  return inject(annotationDraftInjectorKey, null);
}
