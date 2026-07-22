import type { RouteRecordRaw } from 'vue-router';

const routes: RouteRecordRaw[] = [
  { path: '/login', name: 'login', component: () => import('@/pages/LoginPage.vue') },
  {
    path: '/',
    component: () => import('@/layouts/MainLayout.vue'),
    children: [
      { path: '', name: 'overview', component: () => import('@/pages/IndexPage.vue') },
      { path: 'new', name: 'new-session', component: () => import('@/pages/NewSessionPage.vue') },
      { path: 'settings', name: 'settings', component: () => import('@/pages/SettingsPage.vue') },
      {
        path: 'projects/new',
        name: 'project-create',
        component: () => import('@/pages/ProjectDirectoryPage.vue'),
      },
      {
        path: 'projects/:projectId/settings',
        name: 'project-settings',
        component: () => import('@/pages/ProjectSettingsPage.vue'),
      },
      { path: 'sessions', name: 'sessions', component: () => import('@/pages/SessionsPage.vue') },
      {
        path: 'sessions/:id',
        name: 'session-detail',
        component: () => import('@/pages/SessionDetailPage.vue'),
      },
      {
        path: 'sessions/:id/commits',
        name: 'session-commits',
        component: () => import('@/pages/CommitHistoryPage.vue'),
      },
      {
        path: 'sessions/:id/artifacts',
        name: 'session-artifacts',
        component: () => import('@/pages/SessionArtifactsPage.vue'),
      },
      {
        path: 'sessions/:id/prompts/:promptId/edit',
        name: 'prompt-append-edit',
        component: () => import('@/pages/PromptAppendEditPage.vue'),
      },
      {
        path: 'sessions/:id/artifacts/:fileId',
        name: 'session-artifact',
        component: () => import('@/pages/SessionArtifactPage.vue'),
      },
      { path: 'diff', name: 'diff', component: () => import('@/pages/DiffPage.vue') },
      {
        path: 'projects/:projectId/workflow',
        name: 'workflow-config',
        component: () => import('@/pages/WorkflowConfigPage.vue'),
      },
    ],
  },

  // Always leave this as last one,
  // but you can also remove it
  {
    path: '/:catchAll(.*)*',
    component: () => import('@/pages/ErrorNotFound.vue'),
  },
];

export default routes;
