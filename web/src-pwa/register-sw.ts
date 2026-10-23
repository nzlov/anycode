import { applicationBasePath } from '@/services/applicationPath.js';
import { register } from 'register-service-worker';

// Temporary local previews must not register a worker on the host application origin.
if (applicationBasePath()) {
  document.querySelector('link[rel="manifest"]')?.remove();
} else {
  register(import.meta.env.QUASAR_SERVICE_WORKER_FILE, {
    error(error) {
      console.error('Service worker registration failed', error);
    },
  });
}
