export type SessionMode = 'workflow' | 'chat';
export type SessionStatus = 'running' | 'waiting_user' | 'waiting_approval' | 'stopped' | 'blocked' | 'completed';

export interface ProjectSummary {
  id: string;
  name: string;
  path: string;
  active: boolean;
  defaultBranch: string;
  openSessions: number;
}

export interface SessionCard {
  id: string;
  projectId: string;
  title: string;
  summary: string;
  mode: SessionMode;
  status: SessionStatus;
  branch: string;
  node: string;
  updatedAt: string;
  pendingQuestion: boolean;
  filesChanged: number;
}

export interface SessionEvent {
  id: string;
  kind: 'thought' | 'tool' | 'assistant' | 'status' | 'question';
  rawType: string;
  title: string;
  body: string;
  time: string;
}

export interface DiffFile {
  path: string;
  status: 'modified' | 'added' | 'deleted';
  additions: number;
  deletions: number;
  hunks: string[];
}

export const projects: ProjectSummary[] = [
  {
    id: 'anycode',
    name: 'anycode',
    path: '/workspaces/anycode',
    active: true,
    defaultBranch: 'main',
    openSessions: 4,
  },
  {
    id: 'openchamber',
    name: 'openchamber',
    path: '/workspaces/openchamber',
    active: false,
    defaultBranch: 'main',
    openSessions: 2,
  },
  {
    id: 'pets',
    name: 'pets',
    path: '/workspaces/pets',
    active: false,
    defaultBranch: 'master',
    openSessions: 1,
  },
];

export const sessions: SessionCard[] = [
  {
    id: 'session-answer-user',
    projectId: 'anycode',
    title: '实现 answer_user 选项回答',
    summary: '会话详情页展示事件流，右侧显示会话信息和当前分支变更。',
    mode: 'workflow',
    status: 'running',
    branch: 'feature/answer-user',
    node: '验证构建结果',
    updatedAt: '今天 09:42',
    pendingQuestion: false,
    filesChanged: 6,
  },
  {
    id: 'session-quasar-shell',
    projectId: 'anycode',
    title: '扩展 Quasar 前端骨架',
    summary: '补齐总揽、会话详情、Diff、流程配置和新建卡片入口。',
    mode: 'chat',
    status: 'waiting_user',
    branch: 'dev-foundation',
    node: '待确认 UI 交互',
    updatedAt: '今天 08:18',
    pendingQuestion: true,
    filesChanged: 12,
  },
  {
    id: 'session-runtime',
    projectId: 'openchamber',
    title: 'OpenCode runtime 替换为 Codex runtime',
    summary: '复核 SSE 事件顺序和模型参数透传。',
    mode: 'workflow',
    status: 'completed',
    branch: 'codex-runtime',
    node: '已完成',
    updatedAt: '昨天 18:10',
    pendingQuestion: false,
    filesChanged: 18,
  },
  {
    id: 'session-questionbank',
    projectId: 'pets',
    title: 'questionbank 增加去重导入检查',
    summary: '导入文件夹时跳过重复题目并保留统计。',
    mode: 'chat',
    status: 'stopped',
    branch: 'main',
    node: '已停止',
    updatedAt: '2026-06-29',
    pendingQuestion: false,
    filesChanged: 4,
  },
  {
    id: 'session-turso-plan',
    projectId: 'anycode',
    title: '生成 Turso 迁移风险计划',
    summary: '分析 ent schema 与 libSQL 连接策略，输出可验证 TODO。',
    mode: 'workflow',
    status: 'blocked',
    branch: 'main',
    node: '等待数据库凭据',
    updatedAt: '2026-06-28',
    pendingQuestion: true,
    filesChanged: 2,
  },
];

export const sessionEvents: SessionEvent[] = [
  {
    id: 'evt-1',
    kind: 'status',
    rawType: 'session.running',
    title: '流程节点启动',
    body: '进入“验证构建结果”，绑定当前 NodeRun attempt。',
    time: '09:42',
  },
  {
    id: 'evt-2',
    kind: 'thought',
    rawType: 'process.codex_event',
    title: '思考',
    body: '先确认前端路由和主题入口，再补齐 mock 页面，避免提前接 GraphQL。',
    time: '09:43',
  },
  {
    id: 'evt-3',
    kind: 'tool',
    rawType: 'process.codex_event',
    title: '工具调用',
    body: 'npm --prefix web run lint',
    time: '09:44',
  },
  {
    id: 'evt-4',
    kind: 'assistant',
    rawType: 'process.codex_event',
    title: '模型输出',
    body: '已完成页面骨架，等待构建验证结果。',
    time: '09:45',
  },
  {
    id: 'evt-5',
    kind: 'question',
    rawType: 'session.waiting_user',
    title: '待回答',
    body: '请选择下一步动作：继续修复、暂停、或进入收尾。',
    time: '09:46',
  },
];

export const diffFiles: DiffFile[] = [
  {
    path: 'web/src/layouts/MainLayout.vue',
    status: 'modified',
    additions: 142,
    deletions: 38,
    hunks: [
      '@@ -1,8 +1,12 @@',
      '+ <q-header bordered class="app-header">',
      '+ <q-btn-toggle v-model="themeMode" :options="themeOptions" />',
      '- <q-badge outline color="primary" label="本机 Codex" />',
    ],
  },
  {
    path: 'web/src/pages/SessionDetailPage.vue',
    status: 'added',
    additions: 188,
    deletions: 0,
    hunks: [
      '@@ -0,0 +1,8 @@',
      '+ <q-page class="detail-page">',
      '+ <section class="event-stream">',
      '+ <q-tabs v-model="rightPanelTab" />',
    ],
  },
  {
    path: 'web/src/theme/tokens.ts',
    status: 'added',
    additions: 64,
    deletions: 0,
    hunks: [
      '@@ -0,0 +1,7 @@',
      '+ export type ThemeMode = "system" | "light" | "dark";',
      '+ export const themeTokens = {',
      '+   quasar: { primary: "#2563eb" }',
    ],
  },
];

export const directoryTree = [
  {
    label: 'workspaces',
    icon: 'folder',
    children: [
      { label: 'anycode', icon: 'folder_open', selectable: true },
      { label: 'openchamber', icon: 'folder', selectable: true },
      { label: 'pets', icon: 'folder', selectable: true },
    ],
  },
];

export function getProjectName(projectId: string) {
  return projects.find((project) => project.id === projectId)?.name ?? projectId;
}

export function getSessionById(sessionId: string) {
  const fallback = sessions[0];
  if (!fallback) {
    throw new Error('mock sessions must contain at least one session');
  }
  return sessions.find((session) => session.id === sessionId) ?? fallback;
}
