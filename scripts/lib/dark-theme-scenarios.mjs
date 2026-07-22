export const darkThemeViewports = [
  { id: 'desktop', width: 1440, height: 900 },
  { id: 'tablet', width: 900, height: 900 },
  { id: 'mobile', width: 390, height: 844 },
];

export const darkThemeRoutes = [
  { id: 'login', path: '/#/login' },
  { id: 'overview', path: '/#/' },
  { id: 'sessions', path: '/#/sessions' },
  { id: 'session-detail', path: '/#/sessions/:sessionId' },
  { id: 'commit-history', path: '/#/sessions/:sessionId/commits' },
  { id: 'diff', path: '/#/diff?sessionId=:sessionId' },
  { id: 'workflow', path: '/#/projects/:projectId/workflow' },
  { id: 'not-found', path: '/#/theme-audit-not-found' },
];

export const darkThemeDialogs = [
  'new-session',
  'prompt-attachment-preview',
  'global-settings',
  'project-directory',
  'project-settings',
  'remove-project-confirmation',
  'logout-confirmation',
  'questions',
  'forward-approval',
  'overview-diff',
  'edit-prompt-append',
  'session-artifact-preview',
  'timeline-artifact-preview',
  'delete-artifact-confirmation',
];

export const darkThemeMenus = [
  'header-more',
  'global-project-actions',
  'overview-todo',
  'overview-context',
  'prompt-config',
  'quick-reply',
];

export const darkThemeSurfaceStates = {
  'route-diff': ['all', 'single'],
  'dialog-global-settings': ['projects', 'quick-commands'],
  'dialog-questions': ['questions', 'diff'],
  'dialog-forward-approval': ['result', 'diff'],
};

export const darkThemeSurfaceViewports = {
  'menu-prompt-config': ['tablet', 'mobile'],
};

export function darkThemeRequiredCaptures() {
  const surfaces = [
    ...darkThemeRoutes.map((route) => `route-${route.id}`),
    ...darkThemeDialogs.map((dialog) => `dialog-${dialog}`),
    ...darkThemeMenus.map((menu) => `menu-${menu}`),
    'overlay-select-popup',
    'overlay-tooltip',
    'overlay-notification',
  ];
  return surfaces.flatMap((surfaceId) =>
    (darkThemeSurfaceStates[surfaceId] || ['default']).flatMap((stateId) =>
      darkThemeViewports
        .filter((viewport) =>
          (darkThemeSurfaceViewports[surfaceId] || darkThemeViewports.map((item) => item.id))
            .includes(viewport.id),
        )
        .map((viewport) => ({ surfaceId, stateId, viewport: viewport.id })),
    ),
  );
}

export function darkThemeScenarioManifest() {
  return {
    version: 1,
    viewports: darkThemeViewports,
    routes: darkThemeRoutes,
    dialogs: darkThemeDialogs,
    menus: darkThemeMenus,
    dynamicOverlays: ['select-popup', 'tooltip', 'notification'],
    surfaceStates: darkThemeSurfaceStates,
    surfaceViewports: darkThemeSurfaceViewports,
    requiredCaptures: darkThemeRequiredCaptures(),
  };
}
