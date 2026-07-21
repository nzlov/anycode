import { graphqlFetch } from '@/services/graphqlClient';

const subscriptionStorageKey = 'anycode.push-subscription-id';

interface WebPushConfig {
  enabled: boolean;
  publicKey: string;
}

export interface PushNotificationState {
  supported: boolean;
  available: boolean;
  permission: NotificationPermission | 'unsupported';
  enabled: boolean;
}

export async function getPushNotificationState(): Promise<PushNotificationState> {
  if (!browserSupportsPush()) return unsupportedState();
  const config = await getWebPushConfig();
  const registration = await navigator.serviceWorker.ready;
  let subscription = await registration.pushManager.getSubscription();
  if (Notification.permission !== 'granted') {
    await subscription?.unsubscribe();
    await unregisterStoredSubscription();
    subscription = null;
  }
  if (
    subscription &&
    Notification.permission === 'granted' &&
    !subscriptionUsesKey(subscription, config.publicKey)
  ) {
    await subscription.unsubscribe();
    await unregisterStoredSubscription();
    subscription = await registration.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: base64URLBytes(config.publicKey),
    });
  }
  if (!subscription) await unregisterStoredSubscription();
  if (subscription && Notification.permission === 'granted') {
    await registerBrowserSubscription(subscription);
  }
  return {
    supported: true,
    available: config.enabled,
    permission: Notification.permission,
    enabled: Boolean(subscription && Notification.permission === 'granted'),
  };
}

export async function enablePushNotifications(): Promise<PushNotificationState> {
  if (!browserSupportsPush()) return unsupportedState();
  const config = await getWebPushConfig();
  if (!config.enabled) {
    return {
      supported: true,
      available: false,
      permission: Notification.permission,
      enabled: false,
    };
  }
  const permission = await Notification.requestPermission();
  if (permission !== 'granted') {
    return { supported: true, available: true, permission, enabled: false };
  }
  const registration = await navigator.serviceWorker.ready;
  let subscription = await registration.pushManager.getSubscription();
  if (subscription && !subscriptionUsesKey(subscription, config.publicKey)) {
    await subscription.unsubscribe();
    await unregisterStoredSubscription();
    subscription = null;
  }
  if (!subscription) await unregisterStoredSubscription();
  subscription ??= await registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: base64URLBytes(config.publicKey),
  });
  await registerBrowserSubscription(subscription);
  return { supported: true, available: true, permission, enabled: true };
}

export async function disablePushNotifications() {
  if (typeof window === 'undefined') return;
  const registration = browserSupportsPush()
    ? await navigator.serviceWorker.getRegistration()
    : undefined;
  const subscription = await registration?.pushManager.getSubscription();
  const results = await Promise.allSettled([
    subscription?.unsubscribe() ?? Promise.resolve(true),
    unregisterStoredSubscription(),
  ]);
  const failure = results.find((result) => result.status === 'rejected');
  if (failure?.status === 'rejected') {
    throw failure.reason;
  }
}

function browserSupportsPush() {
  return (
    typeof window !== 'undefined' &&
    window.isSecureContext &&
    'serviceWorker' in navigator &&
    'PushManager' in window &&
    'Notification' in window
  );
}

function unsupportedState(): PushNotificationState {
  return { supported: false, available: false, permission: 'unsupported', enabled: false };
}

async function getWebPushConfig() {
  const data = await graphqlFetch<{ webPushConfig: WebPushConfig }>({
    query: `
      query WebPushConfig {
        webPushConfig { enabled publicKey }
      }
    `,
    notify: false,
  });
  return data.webPushConfig;
}

async function registerBrowserSubscription(subscription: PushSubscription) {
  const json = subscription.toJSON();
  if (!json.endpoint || !json.keys?.p256dh || !json.keys.auth) {
    throw new Error('浏览器推送订阅无效');
  }
  const data = await graphqlFetch<
    { registerPushSubscription: { id: string } },
    { input: { endpoint: string; p256dh: string; auth: string } }
  >({
    query: `
      mutation RegisterPushSubscription($input: RegisterPushSubscriptionInput!) {
        registerPushSubscription(input: $input) { id }
      }
    `,
    variables: {
      input: { endpoint: json.endpoint, p256dh: json.keys.p256dh, auth: json.keys.auth },
    },
    notify: false,
  });
  window.localStorage.setItem(subscriptionStorageKey, data.registerPushSubscription.id);
}

async function unregisterBrowserSubscription(id: string) {
  await graphqlFetch<{ unregisterPushSubscription: boolean }, { id: string }>({
    query: `
      mutation UnregisterPushSubscription($id: ID!) {
        unregisterPushSubscription(id: $id)
      }
    `,
    variables: { id },
    notify: false,
  });
}

async function unregisterStoredSubscription() {
  const id = window.localStorage.getItem(subscriptionStorageKey);
  if (!id) return;
  await unregisterBrowserSubscription(id);
  window.localStorage.removeItem(subscriptionStorageKey);
}

function subscriptionUsesKey(subscription: PushSubscription, publicKey: string) {
  const current = subscription.options.applicationServerKey;
  if (!current) return false;
  const expected = base64URLBytes(publicKey);
  const actual = new Uint8Array(current);
  return (
    actual.length === expected.length && actual.every((value, index) => value === expected[index])
  );
}

function base64URLBytes(value: string) {
  const padding = '='.repeat((4 - (value.length % 4)) % 4);
  const decoded = window.atob((value + padding).replace(/-/g, '+').replace(/_/g, '/'));
  return Uint8Array.from(decoded, (character) => character.charCodeAt(0));
}
