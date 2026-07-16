<template>
  <div class="error-not-found-page fullscreen text-center q-pa-md flex flex-center">
    <div>
      <div style="font-size: 30vh">404</div>

      <div class="text-h2" style="opacity: 0.4">Oops. Nothing here...</div>

      <q-btn
        class="q-mt-xl error-not-found-page__action"
        unelevated
        to="/"
        label="Go Home"
        no-caps
      />
    </div>
  </div>
</template>

<style scoped>
.error-not-found-page {
  color: var(--ac-status-info-text);
  background: var(--ac-status-info-bg);
}

.error-not-found-page__action {
  color: var(--ac-status-info-bg);
  background: var(--ac-status-info-text);
}
</style>
