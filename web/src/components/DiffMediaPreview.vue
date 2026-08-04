<template>
  <div class="diff-media-preview" :class="`diff-media-preview--${kind}`">
    <section v-for="version in versions" :key="version" class="diff-media-preview__version">
      <div class="diff-media-preview__label text-caption text-weight-medium">
        {{ version === 'old' ? '旧版本' : '新版本' }}
      </div>
      <div v-if="states[version].loading" class="diff-media-preview__state">
        <q-spinner color="primary" size="28px" />
      </div>
      <q-banner v-else-if="states[version].error" dense class="app-feedback app-feedback--danger">
        {{ states[version].error }}
      </q-banner>
      <img
        v-else-if="kind === 'image'"
        :src="states[version].url"
        :alt="`${version === 'old' ? '旧' : '新'}版本 ${filePath}`"
        class="diff-media-preview__image"
      />
      <video
        v-else-if="kind === 'video'"
        :src="states[version].url"
        class="diff-media-preview__video"
        controls
        preload="metadata"
      />
      <iframe
        v-else-if="kind === 'pdf'"
        :src="states[version].url"
        class="diff-media-preview__frame"
        :title="`${version === 'old' ? '旧' : '新'}版本 PDF ${filePath}`"
      />
      <ModelFilePreview
        v-else-if="kind === 'model'"
        :src="states[version].url"
        :filename="filePath"
        class="diff-media-preview__model"
      />
      <audio
        v-else
        :src="states[version].url"
        class="diff-media-preview__audio"
        controls
        preload="metadata"
      />
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, reactive, watch } from 'vue';

import { fetchDiffMedia } from '@/services/diffMedia';
import { diffMediaVersions } from '@/services/diffMediaModel';
import type { DiffMediaKind, DiffMediaVersion } from '@/services/diffMediaModel';

const ModelFilePreview = defineAsyncComponent(() => import('@/components/ModelFilePreview.vue'));

const props = defineProps<{
  sessionId: string;
  filePath: string;
  status: string;
  kind: DiffMediaKind;
}>();

interface VersionState {
  loading: boolean;
  error: string;
  url: string;
}

const states = reactive<Record<DiffMediaVersion, VersionState>>({
  old: { loading: false, error: '', url: '' },
  new: { loading: false, error: '', url: '' },
});
let controller: AbortController | null = null;
const versions = computed(() => diffMediaVersions(props.status));

function clear() {
  controller?.abort();
  controller = null;
  for (const state of Object.values(states)) {
    if (state.url) URL.revokeObjectURL(state.url);
    state.loading = false;
    state.error = '';
    state.url = '';
  }
}

async function load() {
  clear();
  const request = new AbortController();
  controller = request;
  await Promise.all(
    versions.value.map(async (version) => {
      const state = states[version];
      state.loading = true;
      try {
        const blob = await fetchDiffMedia(props.sessionId, props.filePath, version, request.signal);
        if (controller !== request) return;
        state.url = URL.createObjectURL(blob);
      } catch (err) {
        if (controller === request && !request.signal.aborted) {
          state.error = err instanceof Error ? err.message : '读取多媒体版本失败';
        }
      } finally {
        if (controller === request) state.loading = false;
      }
    }),
  );
  if (controller === request) controller = null;
}

watch(() => [props.sessionId, props.filePath, props.status], load, { immediate: true });
onBeforeUnmount(clear);
</script>

<style scoped>
.diff-media-preview {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.diff-media-preview--audio {
  grid-template-columns: repeat(auto-fit, minmax(min(280px, 100%), 1fr));
}

.diff-media-preview__version {
  display: grid;
  min-width: 0;
  align-content: start;
  gap: 8px;
}

.diff-media-preview__label {
  color: var(--ac-text-muted);
}

.diff-media-preview__state {
  display: grid;
  min-height: 120px;
  place-items: center;
}

.diff-media-preview__image,
.diff-media-preview__video {
  width: 100%;
  max-height: 60vh;
  border-radius: 6px;
  background: var(--ac-diff-bg);
  object-fit: contain;
}

.diff-media-preview__frame {
  width: 100%;
  height: min(60vh, 720px);
  border: 0;
  border-radius: 6px;
  background: var(--ac-diff-bg);
}

.diff-media-preview__audio {
  width: 100%;
}

.diff-media-preview__model {
  min-height: min(60vh, 520px);
  border-radius: 6px;
  background: var(--ac-diff-bg);
}

@media (max-width: 720px) {
  .diff-media-preview {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
