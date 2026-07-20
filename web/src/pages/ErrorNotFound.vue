<template>
  <div class="error-not-found-page fullscreen text-center q-pa-md flex flex-center">
    <div class="error-not-found-page__content">
      <div class="error-not-found-page__code">404</div>

      <div class="text-h2 error-not-found-page__message">Oops. Nothing here...</div>

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
  color: var(--ac-text);
  background: var(--ac-page-layer);
}

.error-not-found-page__code {
  font-size: min(30vh, 280px);
  line-height: 1;
}

.error-not-found-page__message {
  color: var(--ac-text-muted);
}

.error-not-found-page__action {
  color: var(--ac-status-info-bg);
  background: var(--ac-status-info-text);
}

@media (max-width: 699px) {
  .error-not-found-page__content {
    padding-bottom: 88px;
  }

  .error-not-found-page__code {
    font-size: 160px;
  }

  .error-not-found-page__message {
    font-size: 40px;
    line-height: 1.2;
  }
}
</style>
