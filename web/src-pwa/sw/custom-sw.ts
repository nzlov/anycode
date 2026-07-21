import { clientsClaim } from 'workbox-core';
import {
  cleanupOutdatedCaches,
  createHandlerBoundToURL,
  precacheAndRoute,
} from 'workbox-precaching';
import { NavigationRoute, registerRoute } from 'workbox-routing';

declare const self: ServiceWorkerGlobalScope & typeof globalThis;

interface CardNotificationPayload {
  title: string;
  body: string;
  sessionId: string;
  status: string;
  path: string;
  tag: string;
}

void self.skipWaiting();
clientsClaim();
precacheAndRoute(self.__WB_MANIFEST);
cleanupOutdatedCaches();

if (import.meta.env.QUASAR_PROD) {
  registerRoute(
    new NavigationRoute(createHandlerBoundToURL(import.meta.env.QUASAR_PWA_FALLBACK_HTML), {
      denylist: [new RegExp(import.meta.env.QUASAR_PWA_SERVICE_WORKER_REGEX), /workbox-(.)*\.js$/],
    }),
  );
}

self.addEventListener('push', (event) => {
  const payload = parsePayload(event.data);
  if (!payload) return;
  event.waitUntil(showCardNotification(payload));
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const path = notificationPath(event.notification.data);
  event.waitUntil(openNotificationPath(path));
});

async function showCardNotification(payload: CardNotificationPayload) {
  const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
  const target = new URL(payload.path, self.location.origin);
  if (
    windows.some((client) => {
      const current = new URL(client.url);
      return client.visibilityState === 'visible' && current.hash === target.hash;
    })
  ) {
    return;
  }
  await self.registration.showNotification(payload.title, {
    body: payload.body,
    tag: payload.tag,
    icon: '/icons/favicon-96x96.png',
    badge: '/icons/favicon-96x96.png',
    data: { path: payload.path, sessionId: payload.sessionId, status: payload.status },
  });
}

async function openNotificationPath(path: string) {
  const target = new URL(path, self.location.origin).href;
  const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
  for (const client of windows) {
    if (new URL(client.url).origin !== self.location.origin) continue;
    await client.focus();
    await client.navigate(target);
    return;
  }
  await self.clients.openWindow(target);
}

function parsePayload(data: PushMessageData | null): CardNotificationPayload | null {
  if (!data) return null;
  try {
    const value = data.json() as Partial<CardNotificationPayload>;
    if (
      typeof value.title !== 'string' ||
      typeof value.body !== 'string' ||
      typeof value.sessionId !== 'string' ||
      typeof value.status !== 'string' ||
      typeof value.path !== 'string' ||
      typeof value.tag !== 'string' ||
      !value.path.startsWith('/#/sessions/')
    ) {
      return null;
    }
    return value as CardNotificationPayload;
  } catch {
    return null;
  }
}

function notificationPath(data: unknown) {
  if (!data || typeof data !== 'object') return '/#/';
  const path = Reflect.get(data, 'path');
  return typeof path === 'string' && path.startsWith('/#/sessions/') ? path : '/#/';
}
