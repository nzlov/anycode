<template>
  <q-page
    class="workbench-page page-shell"
    :class="{ 'workbench-page--desktop-focus': showDesktopFocusLayout }"
  >
    <PageToolbar title="AnyCode">
      <div class="overview-filter-toolbar">
        <div
          v-if="projectChips.length"
          class="overview-project-filters"
          role="group"
          aria-label="项目卡片显示筛选"
        >
          <q-chip
            v-for="project in projectChips"
            :key="project.id"
            clickable
            :outline="!isProjectVisible(project.id)"
            class="overview-project-chip"
            :class="{
              'overview-project-chip--visible': isProjectVisible(project.id),
              'overview-project-chip--hidden': !isProjectVisible(project.id),
            }"
            :icon="isProjectVisible(project.id) ? 'visibility' : 'visibility_off'"
            :aria-pressed="isProjectVisible(project.id)"
            :aria-label="`${isProjectVisible(project.id) ? '隐藏' : '显示'} ${project.name} 项目卡片`"
            @click="toggleProjectVisibility(project.id)"
          >
            {{ project.name }}
          </q-chip>
        </div>
      </div>
    </PageToolbar>

    <section class="overview-card-section">
      <div v-if="visibleLatestCards.length > 0" class="overview-card-grid">
        <q-card
          v-for="card in visibleLatestCards"
          :key="card.id"
          flat
          bordered
          clickable
          tabindex="0"
          class="overview-session-card cursor-pointer"
          :class="overviewCardClass(card)"
          @click="openSessionCard(card.id)"
          @touchend="releaseCardContextMenuTouch(card.id)"
          @touchcancel="clearCardClickSuppression(card.id)"
          @keyup.enter.self="openSessionCard(card.id)"
          @keyup.space.self.prevent="openSessionCard(card.id)"
        >
          <q-card-section class="overview-session-card__body">
            <div class="overview-card-chips">
              <q-badge
                rounded
                class="lane-status-chip"
                :class="statusChipClass(card.status)"
                :label="statusLabel(card.status)"
              />
              <q-badge rounded class="lane-mode-chip" :label="modeLabel(card.mode)" />
              <q-badge rounded class="lane-mode-chip" :label="priorityLabel(card.priority)" />
            </div>

            <div class="overview-card-title">{{ card.title }}</div>

            <div class="overview-card-meta-grid">
              <div>
                <span class="overview-card-meta__label">所属项目</span>
                <span>{{ card.projectName }}</span>
              </div>
              <div>
                <span class="overview-card-meta__label">基础分支</span>
                <span>{{ card.branch }}</span>
              </div>
              <div>
                <span class="overview-card-meta__label">工作分支</span>
                <span>{{ card.worktreeBranch || '-' }}</span>
              </div>
              <div>
                <span class="overview-card-meta__label">当前节点</span>
                <span>{{ card.node }}</span>
              </div>
              <div>
                <span class="overview-card-meta__label">最近操作</span>
                <span>{{ card.updatedAt }}</span>
              </div>
              <div v-if="card.usage">
                <span class="overview-card-meta__label">Token 用量</span>
                <TokenUsageDisplay :usage="card.usage" />
              </div>
            </div>

            <div class="overview-card-footer">
              <div class="overview-card-secondary-actions" @contextmenu.stop @touchstart.stop>
                <q-btn
                  v-if="card.todoList"
                  flat
                  dense
                  no-caps
                  class="overview-todo-btn app-command-btn"
                  icon="checklist"
                  :label="`${card.todoList.completed}/${card.todoList.total}`"
                  aria-label="查看 TODO List"
                  @pointerenter="openTodoMenu(card.id, $event)"
                  @pointerleave="scheduleTodoMenuClose(card.id, $event)"
                  @click.stop="toggleTodoMenu(card.id)"
                  @contextmenu.stop
                  @touchstart.stop
                  @keyup.enter.stop
                  @keyup.space.stop
                >
                  <q-menu
                    no-parent-event
                    no-focus
                    anchor="top left"
                    self="bottom left"
                    class="overview-todo-menu"
                    :model-value="activeTodoMenuId === card.id"
                    @update:model-value="syncTodoMenuModel(card.id, $event)"
                    @click.stop
                    @pointerenter="openTodoMenu(card.id, $event)"
                    @pointerleave="scheduleTodoMenuClose(card.id, $event)"
                  >
                    <q-list dense separator class="app-touch-list">
                      <q-item
                        v-for="(item, index) in card.todoList.items"
                        :key="`${card.id}-${index}`"
                      >
                        <q-item-section avatar>
                          <q-icon
                            :name="item.completed ? 'check_circle' : 'radio_button_unchecked'"
                            :class="item.completed ? 'text-positive' : 'text-muted'"
                          />
                        </q-item-section>
                        <q-item-section>
                          <q-item-label :class="{ 'overview-todo-item--done': item.completed }">
                            {{ item.text }}
                          </q-item-label>
                        </q-item-section>
                      </q-item>
                    </q-list>
                  </q-menu>
                </q-btn>

                <q-btn
                  v-if="card.filesChanged > 0"
                  flat
                  dense
                  no-caps
                  class="overview-diff-btn app-command-btn"
                  icon="difference"
                  :label="String(card.filesChanged)"
                  :aria-label="`查看 ${card.filesChanged} 个变更文件`"
                  @click.stop="openDiffDialog(card)"
                  @keyup.enter.stop
                  @keyup.space.stop
                >
                  <q-tooltip>查看 Diff</q-tooltip>
                </q-btn>

                <q-btn
                  v-if="card.artifactCount > 0"
                  flat
                  dense
                  no-caps
                  class="overview-artifact-btn app-command-btn"
                  icon="inventory_2"
                  :label="String(card.artifactCount)"
                  :aria-label="`查看 ${card.artifactCount} 个临时文件`"
                  @click.stop="openArtifactDialog(card)"
                  @keyup.enter.stop
                  @keyup.space.stop
                >
                  <q-tooltip>查看临时文件</q-tooltip>
                </q-btn>
              </div>

              <div class="overview-card-actions" @contextmenu.stop @touchstart.stop>
                <q-btn
                  v-if="card.status === 'waiting_user'"
                  flat
                  dense
                  class="lane-icon-btn app-icon-btn lane-icon-btn--warning"
                  icon="help"
                  aria-label="回答问题"
                  :loading="questionsLoading && activeQuestionSessionId === card.id"
                  @click.stop="openAnswerDialog(card.id)"
                >
                  <q-tooltip>回答待处理问题</q-tooltip>
                </q-btn>
                <q-btn
                  v-if="card.status === 'waiting_approval'"
                  flat
                  dense
                  class="lane-icon-btn app-icon-btn lane-icon-btn--warning"
                  icon="fact_check"
                  aria-label="人工审核"
                  @click.stop="openApprovalDialog(card)"
                >
                  <q-tooltip>人工审核</q-tooltip>
                </q-btn>
                <q-btn
                  v-if="card.status === 'queued' && card.availableActions.includes('stop')"
                  flat
                  dense
                  class="lane-icon-btn app-icon-btn"
                  color="negative"
                  icon="cancel"
                  aria-label="取消排队"
                  :loading="cardActionLoading && activeActionSessionId === card.id"
                  @click.stop="cancelQueuedCard(card)"
                >
                  <q-tooltip>取消排队</q-tooltip>
                </q-btn>
                <q-btn
                  v-if="cardAction(card)"
                  flat
                  dense
                  class="lane-icon-btn app-icon-btn"
                  :color="cardAction(card)?.color"
                  :icon="cardAction(card)?.icon"
                  :aria-label="cardAction(card)?.tooltip"
                  :loading="cardActionLoading && activeActionSessionId === card.id"
                  :disable="cardAction(card)?.disabled"
                  @click.stop="runCardAction(card)"
                >
                  <q-tooltip>{{ cardAction(card)?.tooltip }}</q-tooltip>
                </q-btn>
              </div>
            </div>
          </q-card-section>
          <q-menu
            context-menu
            @before-show="handleCardContextMenuBeforeShow(card.id, $event)"
            @click.stop
          >
            <q-list dense class="overview-card-menu app-touch-list">
              <q-item
                v-close-popup
                clickable
                tag="a"
                :href="router.resolve({ name: 'session-detail', params: { id: card.id } }).href"
                target="_blank"
                rel="noopener noreferrer"
                @click.stop
              >
                <q-item-section avatar>
                  <q-icon name="open_in_new" />
                </q-item-section>
                <q-item-section>在新标签页中打开</q-item-section>
              </q-item>
              <q-separator />
              <q-item-label header>优先级</q-item-label>
              <q-item
                v-for="priority in priorities"
                :key="priority"
                v-close-popup
                clickable
                :active="card.priority === priority"
                :disable="card.status === 'closed'"
                @click.stop="setCardPriority(card, priority)"
              >
                <q-item-section>{{ priorityLabel(priority) }}</q-item-section>
                <q-item-section v-if="card.priority === priority" side>
                  <q-icon name="check" color="primary" />
                </q-item-section>
              </q-item>
              <q-separator />
              <q-item
                v-close-popup
                clickable
                class="text-negative"
                :disable="!card.availableActions.includes('close')"
                @click.stop="closeCard(card)"
              >
                <q-item-section avatar>
                  <q-icon name="close" />
                </q-item-section>
                <q-item-section>关闭卡片</q-item-section>
              </q-item>
            </q-list>
          </q-menu>
        </q-card>
      </div>
    </section>

    <q-banner
      v-if="latestCards.length > 0 && visibleLatestCards.length === 0"
      rounded
      class="empty-lane-banner q-mt-md"
    >
      当前没有显示的卡片
    </q-banner>
    <q-banner v-else-if="latestCards.length === 0" rounded class="empty-lane-banner q-mt-md">
      暂无卡片
    </q-banner>

    <AnswerUserDialog
      v-model="answerDialog"
      :batches="pendingQuestionBatches"
      :loading="questionsLoading"
      :submitting="questionsSubmitting"
      :diff-target="answerDiffTarget"
      :full-diff-route="answerAllDiffRoute"
      @submit="submitAnswers"
    />

    <q-dialog
      v-model="approvalDialog"
      :maximized="$q.screen.lt.sm"
      @hide="handleApprovalDialogClosed"
    >
      <q-card class="forward-approval-dialog app-content-dialog">
        <div class="forward-approval-dialog__tabs">
          <q-tabs v-model="approvalTab" dense align="left" class="text-primary">
            <q-tab name="output" icon="fact_check" label="审核结果" />
            <q-tab name="diff" icon="difference" label="Diff" />
            <q-tab name="artifacts" icon="inventory_2" label="临时文件" />
          </q-tabs>
          <div class="forward-approval-dialog__actions">
            <q-btn
              v-if="approvalTab === 'diff'"
              flat
              round
              dense
              icon="open_in_new"
              aria-label="打开完整 Diff 页面"
              :to="approvalAllDiffRoute"
            >
              <q-tooltip>打开完整 Diff 页面</q-tooltip>
            </q-btn>
            <q-btn v-close-popup flat round dense icon="close" aria-label="关闭人工审核" />
          </div>
        </div>
        <q-separator />
        <q-tab-panels v-model="approvalTab" animated class="forward-approval-dialog__panels">
          <q-tab-panel name="output" class="forward-approval-dialog__panel">
            <WorkflowResultReview
              :phase="approvalPending?.phase ?? null"
              :result="approvalPending?.result ?? null"
              :resolved-artifacts="approvalResolvedArtifacts"
              @open-artifact="openApprovalArtifact"
            />
          </q-tab-panel>
          <q-tab-panel name="diff" class="forward-approval-dialog__panel">
            <DiffWorkspace
              v-if="approvalDialog"
              v-model="approvalDiffWorkspaceState"
              :target="approvalDiffTarget"
            />
          </q-tab-panel>
          <q-tab-panel name="artifacts" class="forward-approval-dialog__panel">
            <SessionArtifactsPanel
              v-if="approvalDialog"
              :session-id="approvalSessionId"
              :focus-request="approvalArtifactFocus"
              @artifact-deleted="refreshApprovalArtifactReferences"
              @artifacts-refreshed="refreshApprovalArtifactReferences"
            />
          </q-tab-panel>
        </q-tab-panels>
        <q-separator />
        <WorkflowApprovalPanel
          v-if="approvalDialog && !approvalLoading"
          :context-available="
            Boolean(approvalContext) && isPendingApprovalReviewable(approvalPending)
          "
          :submitting="approvalSubmitting"
          @submit="submitApproval"
        />
        <q-inner-loading :showing="approvalLoading">
          <q-spinner color="primary" size="32px" />
        </q-inner-loading>
      </q-card>
    </q-dialog>

    <q-dialog v-model="diffDialog" :maximized="$q.screen.lt.sm" @hide="handleDiffDialogClosed">
      <q-card class="overview-diff-dialog app-content-dialog">
        <div class="overview-diff-dialog__header">
          <div class="text-subtitle1 text-weight-bold">Diff</div>
          <div class="overview-diff-dialog__header-actions">
            <q-btn
              flat
              round
              dense
              icon="open_in_new"
              aria-label="打开完整 Diff 页面"
              :to="diffDialogAllDiffRoute"
              @click.stop
            >
              <q-tooltip>打开完整 Diff 页面</q-tooltip>
            </q-btn>
            <q-btn v-close-popup flat round dense icon="close" aria-label="关闭 Diff" />
          </div>
        </div>
        <q-separator />
        <q-card-section class="overview-diff-dialog__body">
          <DiffWorkspace
            v-if="diffDialog"
            v-model="diffDialogWorkspaceState"
            :target="diffDialogTarget"
          />
        </q-card-section>
      </q-card>
    </q-dialog>

    <q-dialog
      v-model="artifactDialog"
      :maximized="$q.screen.lt.sm"
      @hide="handleArtifactDialogClosed"
    >
      <q-card class="overview-artifact-dialog app-content-dialog">
        <div class="overview-artifact-dialog__header">
          <div class="text-subtitle1 text-weight-bold">临时文件</div>
          <q-btn v-close-popup flat round dense icon="close" aria-label="关闭临时文件" />
        </div>
        <q-separator />
        <q-card-section class="overview-artifact-dialog__body">
          <SessionArtifactsPanel
            v-if="artifactDialog"
            :session-id="artifactDialogSessionId"
            inline-preview
          />
        </q-card-section>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { useQuasar } from 'quasar';
import { useRoute, useRouter } from 'vue-router';

import AnswerUserDialog from '@/components/AnswerUserDialog.vue';
import DiffWorkspace from '@/components/DiffWorkspace.vue';
import PageToolbar from '@/components/PageToolbar.vue';
import SessionArtifactsPanel from '@/components/SessionArtifactsPanel.vue';
import TokenUsageDisplay from '@/components/TokenUsageDisplay.vue';
import WorkflowResultReview from '@/components/WorkflowResultReview.vue';
import WorkflowApprovalPanel from '@/components/WorkflowApprovalPanel.vue';
import { useProjects } from '@/composables/useProjects';
import { useSessionUpdates } from '@/composables/useSessionUpdates';
import { useSessionsPage } from '@/composables/useSessionsPage';
import { type DiffWorkspaceState, type DiffWorkspaceTarget } from '@/services/diff';
import { createOverviewCardGroups } from '@/services/overviewCardGroups';
import { createKeyedLatestRequestTracker } from '@/services/sessionEventTimeline';
import { normalizeArtifactLogicalPath } from '@/services/artifactLogicalPath';
import {
  resolveSessionArtifacts,
  type SessionArtifactFocusRequest,
  type SessionFile,
} from '@/services/sessionFiles';
import { sessionStatusLabel as statusLabel } from '@/services/sessionStatusPresentation';
import {
  closeSession,
  executeSession,
  getPendingQuestionBatches,
  getSession,
  getSessionCard,
  stopSession,
  submitQuestionBatch,
  submitWorkflowApproval,
  updateSessionPriority,
  type QuestionAnswerInput,
  type QuestionBatch,
  type PendingApproval,
  type SessionCard,
  type SessionMode,
  type SessionPriority,
  type SessionStatus,
  type SessionUpdateEvent,
} from '@/services/sessions';
import { isPendingApprovalReviewable } from '@/services/workflowApprovalReview';

const route = useRoute();
const router = useRouter();
const $q = useQuasar();
const overviewDesktopMinWidth = 700;
const hiddenProjectStorageKey = 'anycode.overview.hidden-projects.v1';
const todoMenuHideDelay = 120;
const showDesktopFocusLayout = computed(() => $q.screen.width >= overviewDesktopMinWidth);
const projectScopeId = computed(() => {
  const value = route.query.projectId;
  return typeof value === 'string' ? value : '';
});

const {
  rows: latestRows,
  projectId: latestProjectId,
  loadSessions: loadLatestSessions,
} = useSessionsPage({
  projectId: projectScopeId.value,
  range: 'latest',
  page: 1,
  pageSize: 100,
  sort: 'updated_at desc',
  loadAll: true,
});
const { projects, loadProjects } = useProjects();

const hiddenProjectIds = ref(readHiddenProjectIds());
const overviewCardGroups = computed(() => createOverviewCardGroups(latestRows.value, []));
const latestCards = computed(() => overviewCardGroups.value.latestCards);
const projectChips = computed(() => {
  const seen = new Set<string>();
  return latestCards.value
    .filter((card: SessionCard) => {
      if (seen.has(card.projectId)) return false;
      seen.add(card.projectId);
      return true;
    })
    .map((card: SessionCard) => ({
      id: card.projectId,
      name: card.projectName || card.projectId,
    }));
});
const visibleLatestCards = computed(() =>
  latestCards.value.filter((card: SessionCard) => !hiddenProjectIds.value.has(card.projectId)),
);
const activeTodoMenuId = ref('');
const answerDialog = ref(false);
const activeQuestionSessionId = ref('');
const pendingQuestionBatches = ref<QuestionBatch[]>([]);
const questionsLoading = ref(false);
const questionsSubmitting = ref(false);
let questionRequestGeneration = 0;
const approvalDialog = ref(false);
const approvalLoading = ref(false);
const approvalTab = ref<'output' | 'diff' | 'artifacts'>('output');
const approvalSubmitting = ref(false);
const approvalSessionId = ref('');
const approvalContext = ref<ApprovalContext | null>(null);
const approvalPending = ref<PendingApproval | null>(null);
let approvalContextGeneration = 0;
let approvalArtifactResolveRequest = 0;
let approvalArtifactFocusToken = 0;
const approvalResolvedArtifacts = ref<Record<string, SessionFile>>({});
const approvalArtifactFocus = ref<SessionArtifactFocusRequest | null>(null);
const approvalDiffWorkspaceState = ref<DiffWorkspaceState>(initialDiffWorkspaceState());
const diffDialog = ref(false);
const diffDialogSessionId = ref('');
const diffDialogWorkspaceState = ref<DiffWorkspaceState>({
  mode: 'all',
  filePath: '',
});
const artifactDialog = ref(false);
const artifactDialogSessionId = ref('');
const cardActionLoading = ref(false);
const activeActionSessionId = ref('');
const activePrioritySessionId = ref('');
const activeCloseSessionId = ref('');
const priorities: SessionPriority[] = ['high', 'medium', 'low'];
const answerAllDiffRoute = computed(() => ({
  path: '/diff',
  query: { sessionId: activeQuestionSessionId.value, mode: 'all' },
}));
const answerDiffTarget = computed<DiffWorkspaceTarget>(() => ({
  kind: 'session',
  sessionId: activeQuestionSessionId.value,
}));
const approvalAllDiffRoute = computed(() => ({
  path: '/diff',
  query: { sessionId: approvalSessionId.value, mode: 'all' },
}));
const approvalDiffTarget = computed<DiffWorkspaceTarget>(() => ({
  kind: 'session',
  sessionId: approvalSessionId.value,
}));
const diffDialogAllDiffRoute = computed(() => ({
  path: '/diff',
  query: { sessionId: diffDialogSessionId.value, mode: 'all' },
}));
const diffDialogTarget = computed<DiffWorkspaceTarget>(() => ({
  kind: 'session',
  sessionId: diffDialogSessionId.value,
}));
let todoMenuHideTimer: ReturnType<typeof setTimeout> | null = null;
const cardRefreshRequests = createKeyedLatestRequestTracker();
let overviewMounted = false;
// GLUE: suppress Quasar's synthetic post-long-press click; remove when QMenu consumes it upstream.
let suppressedCardClickId = '';
let cardClickSuppressionTimer: ReturnType<typeof setTimeout> | null = null;

const { start: startOverviewLiveUpdates, stop: stopOverviewLiveUpdates } = useSessionUpdates({
  onData: handleSessionUpdate,
});

interface ApprovalContext {
  sessionId: string;
  nodeId: string;
}

onMounted(() => {
  overviewMounted = true;
  void startOverview();
});

onUnmounted(() => {
  overviewMounted = false;
  cardRefreshRequests.clear();
  clearTodoMenuHideTimer();
  clearCardClickSuppression();
  stopOverviewLiveUpdates();
});

watch(projectScopeId, (value) => {
  latestProjectId.value = value;
  void loadOverviewSessions();
});

watch(
  () => projects.value.map((project) => project.id).join('\0'),
  () => pruneHiddenProjectIds(),
);

watch(answerDialog, (open) => {
  if (!open) clearAnswerContext();
});

async function startOverview() {
  await loadProjects();
  pruneHiddenProjectIds();
  await loadOverviewSessions();
  if (!overviewMounted) return;
  startOverviewLiveUpdates();
}

async function loadOverviewSessions() {
  await loadLatestSessions();
}

function readHiddenProjectIds() {
  if (typeof window === 'undefined') return new Set<string>();
  try {
    const stored = JSON.parse(window.localStorage.getItem(hiddenProjectStorageKey) ?? '[]');
    if (!Array.isArray(stored)) return new Set<string>();
    return new Set(stored.filter((id): id is string => typeof id === 'string' && id !== ''));
  } catch {
    return new Set<string>();
  }
}

function persistHiddenProjectIds() {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(
      hiddenProjectStorageKey,
      JSON.stringify([...hiddenProjectIds.value]),
    );
  } catch {
    // Browser storage can be unavailable; filtering still works for the current page.
  }
}

function pruneHiddenProjectIds() {
  const projectIds = new Set(projects.value.map((project) => project.id));
  const next = new Set(
    [...hiddenProjectIds.value].filter((projectId) => projectIds.has(projectId)),
  );
  if (next.size === hiddenProjectIds.value.size) return;
  hiddenProjectIds.value = next;
  persistHiddenProjectIds();
}

function isProjectVisible(projectId: string) {
  return !hiddenProjectIds.value.has(projectId);
}

function toggleProjectVisibility(projectId: string) {
  const next = new Set(hiddenProjectIds.value);
  if (next.has(projectId)) {
    next.delete(projectId);
  } else {
    next.add(projectId);
  }
  hiddenProjectIds.value = next;
  persistHiddenProjectIds();
}

function handleSessionUpdate(update: SessionUpdateEvent) {
  cardRefreshRequests.invalidate(update.sessionId);
  const status = update.status?.status;
  if (activeQuestionSessionId.value === update.sessionId && status && status !== 'waiting_user') {
    answerDialog.value = false;
    clearAnswerContext();
  }
  if (approvalSessionId.value === update.sessionId && status && status !== 'waiting_approval') {
    approvalDialog.value = false;
    clearApprovalContext();
  }
  const index = latestRows.value.findIndex((card) => card.id === update.sessionId);
  if (status === 'closed') {
    if (index >= 0)
      latestRows.value = latestRows.value.filter((card) => card.id !== update.sessionId);
    return;
  }
  if (index < 0) {
    if (update.status) void refreshOverviewCard(update.sessionId);
    return;
  }

  const current = latestRows.value[index];
  if (!current) return;
  let next = current;
  if (update.status) {
    next = {
      ...next,
      status: update.status.status,
      node: update.status.node,
      availableActions: update.status.availableActions,
      updatedAt: update.status.updatedAt,
      updatedTime: update.status.updatedTime,
    };
  }
  if (update.eventType === 'session.todo_list_updated') {
    next = { ...next, todoList: update.todoList ?? null };
  }
  if (typeof update.filesChanged === 'number') {
    next = { ...next, filesChanged: update.filesChanged };
  }
  if (typeof update.artifactCount === 'number') {
    next = { ...next, artifactCount: update.artifactCount };
  }
  if (update.usage) {
    next = { ...next, usage: update.usage };
  }
  if (update.priority) {
    next = { ...next, priority: update.priority };
  }
  if (update.availableActions !== undefined) {
    next = { ...next, availableActions: update.availableActions };
  }
  if (update.updatedAt && update.updatedTime) {
    next = { ...next, updatedAt: update.updatedAt, updatedTime: update.updatedTime };
  }
  if (next !== current) replaceOverviewCard(next);
}

function refreshOverviewCard(sessionId: string): Promise<void> {
  const generation = cardRefreshRequests.next(sessionId);
  return getSessionCard(sessionId, { notify: false })
    .then((card) => {
      if (!overviewMounted || !cardRefreshRequests.isCurrent(sessionId, generation)) return;
      replaceOverviewCard(card);
    })
    .catch(() => {
      // A later event or user action can retry this focused refresh.
    });
}

function replaceOverviewCard(card: SessionCard) {
  const scopedOut = projectScopeId.value && card.projectId !== projectScopeId.value;
  const withoutCurrent = latestRows.value.filter((item) => item.id !== card.id);
  if (scopedOut || card.status === 'closed') {
    latestRows.value = withoutCurrent;
    return;
  }
  latestRows.value = [card, ...withoutCurrent].sort((left, right) =>
    right.updatedTime.localeCompare(left.updatedTime),
  );
}

function modeLabel(mode: SessionMode) {
  return mode === 'workflow' ? '流程模式' : '会话模式';
}

function priorityLabel(priority: SessionPriority) {
  const labels: Record<SessionPriority, string> = {
    high: '高优先级',
    medium: '中优先级',
    low: '低优先级',
  };
  return labels[priority];
}

function statusChipClass(status: SessionStatus) {
  return `lane-status-chip--${status}`;
}

function overviewCardClass(card: SessionCard) {
  return `overview-session-card--${card.status}`;
}

function clearTodoMenuHideTimer() {
  if (!todoMenuHideTimer) return;
  clearTimeout(todoMenuHideTimer);
  todoMenuHideTimer = null;
}

function openTodoMenu(sessionId: string, event: PointerEvent) {
  if (event.pointerType !== 'mouse') return;
  clearTodoMenuHideTimer();
  activeTodoMenuId.value = sessionId;
}

function toggleTodoMenu(sessionId: string) {
  clearTodoMenuHideTimer();
  activeTodoMenuId.value = activeTodoMenuId.value === sessionId ? '' : sessionId;
}

function scheduleTodoMenuClose(sessionId: string, event: PointerEvent) {
  if (event.pointerType !== 'mouse') return;
  clearTodoMenuHideTimer();
  todoMenuHideTimer = setTimeout(() => {
    todoMenuHideTimer = null;
    if (activeTodoMenuId.value === sessionId) activeTodoMenuId.value = '';
  }, todoMenuHideDelay);
}

function syncTodoMenuModel(sessionId: string, showing: boolean) {
  clearTodoMenuHideTimer();
  if (showing) {
    activeTodoMenuId.value = sessionId;
  } else if (activeTodoMenuId.value === sessionId) {
    activeTodoMenuId.value = '';
  }
}

function openSessionCard(sessionId: string) {
  if (suppressedCardClickId === sessionId) {
    clearCardClickSuppression(sessionId);
    return;
  }
  void router.push(`/sessions/${sessionId}`);
}

function handleCardContextMenuBeforeShow(sessionId: string, event: Event) {
  if (event.type !== 'touchstart') return;
  clearCardClickSuppression();
  suppressedCardClickId = sessionId;
}

function releaseCardContextMenuTouch(sessionId: string) {
  if (suppressedCardClickId !== sessionId) return;
  cardClickSuppressionTimer = setTimeout(() => clearCardClickSuppression(sessionId), 500);
}

function clearCardClickSuppression(sessionId?: string) {
  if (sessionId && suppressedCardClickId !== sessionId) return;
  suppressedCardClickId = '';
  if (cardClickSuppressionTimer) {
    clearTimeout(cardClickSuppressionTimer);
    cardClickSuppressionTimer = null;
  }
}

function cardAction(card: SessionCard) {
  if (
    card.status === 'waiting_user' ||
    card.status === 'waiting_approval' ||
    card.status === 'closed'
  ) {
    return null;
  }
  if (card.status === 'starting' || card.status === 'running') {
    return { icon: 'stop', color: 'negative', tooltip: '运行中，点击停止', disabled: false };
  }
  if (card.status === 'stopping') {
    return { icon: 'hourglass_top', color: 'warning', tooltip: '停止中', disabled: true };
  }
  if (card.status === 'queued' && card.availableActions.includes('execute')) {
    return { icon: 'play_arrow', color: 'primary', tooltip: '强制启动排队卡片', disabled: false };
  }
  if (card.availableActions.includes('execute')) {
    return { icon: 'play_arrow', color: 'primary', tooltip: '运行会话', disabled: false };
  }
  return null;
}

async function runCardAction(card: SessionCard) {
  const action = cardAction(card);
  if (!action || action.disabled) return;
  activeActionSessionId.value = card.id;
  cardActionLoading.value = true;
  try {
    if (card.status === 'starting' || card.status === 'running') {
      await stopSession(card.id);
    } else if (card.availableActions.includes('execute')) {
      await executeSession(card.id, card.status === 'queued');
    }
    await refreshOverviewCard(card.id);
  } finally {
    cardActionLoading.value = false;
    activeActionSessionId.value = '';
  }
}

async function cancelQueuedCard(card: SessionCard) {
  if (card.status !== 'queued' || !card.availableActions.includes('stop')) return;
  activeActionSessionId.value = card.id;
  cardActionLoading.value = true;
  try {
    await stopSession(card.id);
    await refreshOverviewCard(card.id);
  } catch {
    await refreshOverviewCard(card.id);
  } finally {
    cardActionLoading.value = false;
    activeActionSessionId.value = '';
  }
}

async function setCardPriority(card: SessionCard, priority: SessionPriority) {
  if (card.status === 'closed' || card.priority === priority || activePrioritySessionId.value)
    return;
  activePrioritySessionId.value = card.id;
  try {
    await updateSessionPriority(card.id, priority);
    await refreshOverviewCard(card.id);
  } finally {
    activePrioritySessionId.value = '';
  }
}

async function closeCard(card: SessionCard) {
  if (!card.availableActions.includes('close') || activeCloseSessionId.value) return;
  activeCloseSessionId.value = card.id;
  try {
    await closeSession(card.id);
    latestRows.value = latestRows.value.filter((item) => item.id !== card.id);
  } finally {
    activeCloseSessionId.value = '';
  }
}

async function openAnswerDialog(sessionId: string) {
  const requestGeneration = ++questionRequestGeneration;
  activeQuestionSessionId.value = sessionId;
  pendingQuestionBatches.value = [];
  questionsLoading.value = true;
  answerDialog.value = true;
  try {
    const batches = await getPendingQuestionBatches(sessionId);
    if (
      requestGeneration === questionRequestGeneration &&
      activeQuestionSessionId.value === sessionId
    ) {
      const card = latestRows.value.find((item) => item.id === sessionId);
      if (card?.status !== 'waiting_user' || batches.length === 0) {
        answerDialog.value = false;
        clearAnswerContext();
        return;
      }
      pendingQuestionBatches.value = batches;
    }
  } finally {
    if (
      requestGeneration === questionRequestGeneration &&
      activeQuestionSessionId.value === sessionId
    ) {
      questionsLoading.value = false;
    }
  }
}

async function submitAnswers(batchId: string, answers: QuestionAnswerInput[]) {
  questionsSubmitting.value = true;
  try {
    await submitQuestionBatch(batchId, answers);
    answerDialog.value = false;
  } finally {
    questionsSubmitting.value = false;
  }
}

async function openApprovalDialog(card: SessionCard) {
  const requestGeneration = ++approvalContextGeneration;
  approvalSessionId.value = card.id;
  approvalTab.value = 'output';
  approvalContext.value = null;
  approvalPending.value = null;
  approvalResolvedArtifacts.value = {};
  approvalArtifactFocus.value = null;
  approvalDiffWorkspaceState.value = initialDiffWorkspaceState();
  approvalLoading.value = true;
  approvalDialog.value = true;
  try {
    const detail = await getSession(card.id);
    if (!isCurrentApprovalContext(requestGeneration, card.id)) return;
    if (detail.status !== 'waiting_approval' || !detail.pendingApproval) {
      approvalDialog.value = false;
      clearApprovalContext();
      return;
    }
    approvalPending.value = detail.pendingApproval ?? null;
    approvalContext.value = detail.pendingApproval
      ? { sessionId: detail.pendingApproval.sessionId, nodeId: detail.pendingApproval.nodeId }
      : null;
    await refreshApprovalArtifactReferences();
  } catch {
    // The GraphQL client already reports interactive query failures.
  } finally {
    if (isCurrentApprovalContext(requestGeneration, card.id)) {
      approvalLoading.value = false;
    }
  }
}

async function refreshApprovalArtifactReferences() {
  const requestGeneration = approvalContextGeneration;
  const request = ++approvalArtifactResolveRequest;
  const sessionId = approvalSessionId.value;
  const logicalPaths = approvalArtifactLogicalPaths(approvalPending.value);
  approvalResolvedArtifacts.value = {};
  if (!sessionId || logicalPaths.length === 0) return;
  try {
    const resolved = await resolveSessionArtifacts(sessionId, logicalPaths);
    if (
      request !== approvalArtifactResolveRequest ||
      approvalContextGeneration !== requestGeneration ||
      approvalSessionId.value !== sessionId
    ) {
      return;
    }
    approvalResolvedArtifacts.value = Object.fromEntries(
      resolved.map((artifact) => [artifact.logicalPath, artifact.file]),
    );
  } catch {
    if (
      request === approvalArtifactResolveRequest &&
      approvalContextGeneration === requestGeneration &&
      approvalSessionId.value === sessionId
    ) {
      approvalResolvedArtifacts.value = {};
    }
  }
}

function approvalArtifactLogicalPaths(pending: PendingApproval | null) {
  const paths: string[] = [];
  const seen = new Set<string>();
  for (const artifact of pending?.result?.artifacts ?? []) {
    const logicalPath = normalizeArtifactLogicalPath(artifact.ref);
    if (!logicalPath || seen.has(logicalPath)) continue;
    seen.add(logicalPath);
    paths.push(logicalPath);
    if (paths.length === 100) break;
  }
  return paths;
}

function openApprovalArtifact(file: SessionFile) {
  approvalTab.value = 'artifacts';
  // GLUE: bridges NodeResult logical refs to SessionFile selection; remove when results store file IDs.
  approvalArtifactFocus.value = { file, token: ++approvalArtifactFocusToken };
}

function handleApprovalDialogClosed() {
  clearApprovalContext();
}

function clearApprovalContext() {
  approvalContextGeneration += 1;
  approvalArtifactResolveRequest += 1;
  approvalContext.value = null;
  approvalPending.value = null;
  approvalLoading.value = false;
  approvalSessionId.value = '';
  approvalResolvedArtifacts.value = {};
  approvalArtifactFocus.value = null;
}

function clearAnswerContext() {
  questionRequestGeneration += 1;
  activeQuestionSessionId.value = '';
  pendingQuestionBatches.value = [];
  questionsLoading.value = false;
}

function isCurrentApprovalContext(requestGeneration: number, sessionId: string) {
  return approvalContextGeneration === requestGeneration && approvalSessionId.value === sessionId;
}

function openDiffDialog(card: SessionCard) {
  diffDialogSessionId.value = card.id;
  diffDialogWorkspaceState.value = { mode: 'all', filePath: '' };
  diffDialog.value = true;
}

function initialDiffWorkspaceState(): DiffWorkspaceState {
  return { mode: 'all', filePath: '' };
}

function handleDiffDialogClosed() {
  diffDialogSessionId.value = '';
}

function openArtifactDialog(card: SessionCard) {
  artifactDialogSessionId.value = card.id;
  artifactDialog.value = true;
}

function handleArtifactDialogClosed() {
  artifactDialogSessionId.value = '';
}

async function submitApproval(approved: boolean, comment: string) {
  if (approvalSubmitting.value) return;
  if (
    !approvalContext.value ||
    !approvalSessionId.value ||
    !isPendingApprovalReviewable(approvalPending.value)
  )
    return;
  const requestGeneration = approvalContextGeneration;
  const cardSessionId = approvalSessionId.value;
  const workflowSessionId = approvalContext.value.sessionId;
  const nodeId = approvalContext.value.nodeId;
  approvalSubmitting.value = true;
  try {
    await submitWorkflowApproval({
      sessionId: workflowSessionId,
      nodeId,
      approved,
      comment,
    });
    if (
      isCurrentApprovalContext(requestGeneration, cardSessionId) &&
      approvalContext.value?.sessionId === workflowSessionId &&
      approvalContext.value?.nodeId === nodeId
    ) {
      approvalDialog.value = false;
    }
    await refreshOverviewCard(cardSessionId);
  } finally {
    approvalSubmitting.value = false;
  }
}
</script>
