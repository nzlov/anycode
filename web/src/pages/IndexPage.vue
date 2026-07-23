<template>
  <q-page
    class="workbench-page page-shell"
    :style-fn="isHorizontalView ? horizontalPageStyle : undefined"
    :class="{
      'workbench-page--desktop-focus': showDesktopFocusLayout,
      'workbench-page--horizontal': isHorizontalView,
    }"
  >
    <PageToolbar title="AnyCode" title-icon="img:/icons/anycode.svg">
      <div v-if="projectChips.length && !$q.screen.lt.sm" class="overview-filter-toolbar">
        <ProjectVisibilityFilters
          :projects="projectChips"
          :hidden-project-ids="hiddenProjectIds"
          @toggle="toggleProjectVisibility"
        />
      </div>
    </PageToolbar>

    <ProjectVisibilityFilters
      v-if="$q.screen.lt.sm && projectChips.length"
      class="overview-project-filters--mobile"
      :projects="projectChips"
      :hidden-project-ids="hiddenProjectIds"
      @toggle="toggleProjectVisibility"
    />

    <section
      v-if="isHorizontalView && visibleLatestCards.length > 0"
      class="overview-horizontal-section"
      aria-label="横向会话详情"
    >
      <div class="overview-horizontal-track">
        <OverviewHorizontalSession
          v-for="card in horizontalCards"
          :key="card.id"
          :card="card"
          :width="sessionColumnWidth(card.id)"
          :min-width="minSessionColumnWidth"
          :priority-loading="activePrioritySessionId === card.id"
          :close-loading="activeCloseSessionId === card.id"
          @update:width="setSessionColumnWidth(card.id, $event)"
          @set-priority="setCardPriority(card, $event)"
          @close="closeCard(card)"
        />
      </div>
    </section>

    <section v-else-if="!isHorizontalView" class="overview-card-section">
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
              <SessionPriorityControl
                :priority="card.priority"
                :loading="activePrioritySessionId === card.id"
                :disabled="card.status === 'closed'"
                @change="setCardPriority(card, $event)"
              />
              <q-badge
                outline
                :color="statusColor(card.status)"
                :label="statusLabel(card.status)"
              />
              <q-badge rounded class="lane-mode-chip" :label="modeBadgeLabel(card.mode)" />
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
              <div v-if="card.mode === 'workflow'">
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
                  @click.stop="openQuestionsDialog(card.id)"
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
          <SessionCardContextMenu
            :card="card"
            :priority-loading="activePrioritySessionId === card.id"
            :close-loading="activeCloseSessionId === card.id"
            @before-show="handleCardContextMenuBeforeShow(card.id, $event)"
            @set-priority="setCardPriority(card, $event)"
            @close="closeCard(card)"
          />
        </q-card>
      </div>
    </section>

    <q-banner
      v-if="!isHorizontalView && latestCards.length > 0 && visibleLatestCards.length === 0"
      rounded
      class="empty-lane-banner q-mt-md"
    >
      当前没有显示的卡片
    </q-banner>
    <q-banner
      v-else-if="!isHorizontalView && latestCards.length === 0"
      rounded
      class="empty-lane-banner q-mt-md"
    >
      暂无卡片
    </q-banner>
    <div
      v-else-if="isHorizontalView && visibleLatestCards.length === 0"
      class="overview-horizontal-empty"
    >
      {{ latestCards.length > 0 ? '当前没有显示的会话' : '暂无会话' }}
    </div>

    <q-page-sticky v-if="!showDesktopFocusLayout" position="bottom-right" :offset="[24, 24]">
      <q-btn
        fab
        color="primary"
        class="app-on-primary"
        icon="add"
        aria-label="新建卡片"
        @click="openNewSession"
      >
        <q-tooltip>新建卡片</q-tooltip>
      </q-btn>
    </q-page-sticky>

    <NewSessionDialog
      v-model="newSessionOpen"
      :default-project-id="projectScopeId"
      :panel="showDesktopFocusLayout"
      @created="refreshOverviewCard"
    />

    <QuestionsDialog
      v-model="questionsDialog"
      :requests="pendingQuestionRequests"
      :loading="questionsLoading"
      :submitting="questionsSubmitting"
      :diff-target="questionsDiffTarget"
      :full-diff-route="questionsAllDiffRoute"
      @submit="submitAnswers"
    />

    <q-dialog v-model="approvalDialog" @hide="handleApprovalDialogClosed">
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

    <q-dialog v-model="diffDialog" @hide="handleDiffDialogClosed">
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

    <q-dialog v-model="artifactDialog" @hide="handleArtifactDialogClosed">
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

import QuestionsDialog from '@/components/QuestionsDialog.vue';
import DiffWorkspace from '@/components/DiffWorkspace.vue';
import NewSessionDialog from '@/components/NewSessionDialog.vue';
import OverviewHorizontalSession from '@/components/OverviewHorizontalSession.vue';
import PageToolbar from '@/components/PageToolbar.vue';
import ProjectVisibilityFilters from '@/components/ProjectVisibilityFilters.vue';
import SessionCardContextMenu from '@/components/SessionCardContextMenu.vue';
import SessionPriorityControl from '@/components/SessionPriorityControl.vue';
import SessionArtifactsPanel from '@/components/SessionArtifactsPanel.vue';
import TokenUsageDisplay from '@/components/TokenUsageDisplay.vue';
import WorkflowResultReview from '@/components/WorkflowResultReview.vue';
import WorkflowApprovalPanel from '@/components/WorkflowApprovalPanel.vue';
import { useOverviewViewMode } from '@/composables/useOverviewViewMode';
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
import { sessionModeBadgeLabel as modeBadgeLabel } from '@/services/sessionModePresentation';
import {
  sessionStatusColor as statusColor,
  sessionStatusLabel as statusLabel,
} from '@/services/sessionStatusPresentation';
import {
  closeSession,
  executeSession,
  getPendingQuestionRequests,
  getSession,
  getSessionCard,
  retrySessionInitialization,
  stopSession,
  submitQuestionRequest,
  submitWorkflowApproval,
  updateSessionPriority,
  type QuestionAnswerInput,
  type QuestionRequest,
  type PendingApproval,
  type SessionCard,
  type SessionPriority,
  type SessionUpdateEvent,
} from '@/services/sessions';
import { isPendingApprovalReviewable } from '@/services/workflowApprovalReview';

const route = useRoute();
const router = useRouter();
const $q = useQuasar();
const overviewDesktopMinWidth = 700;
const hiddenProjectStorageKey = 'anycode.overview.hidden-projects.v1';
const horizontalWidthStorageKey = 'anycode.overview.horizontal-widths.v1';
const minSessionColumnWidth = 320;
const todoMenuHideDelay = 120;
const { overviewViewMode } = useOverviewViewMode();
const isDesktopOverview = computed(() => $q.screen.width >= overviewDesktopMinWidth);
const isHorizontalView = computed(
  () => isDesktopOverview.value && overviewViewMode.value === 'horizontal',
);
const showDesktopFocusLayout = computed(() => isDesktopOverview.value && !isHorizontalView.value);

function horizontalPageStyle(offset: number, height: number) {
  return { height: `${Math.max(0, height - offset)}px` };
}

const newSessionOpen = ref(false);
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
const horizontalCards = computed(() =>
  overviewCardGroups.value.horizontalCards.filter(
    (card: SessionCard) => !hiddenProjectIds.value.has(card.projectId),
  ),
);
const sessionColumnWidths = ref(readSessionColumnWidths());
const activeTodoMenuId = ref('');
const questionsDialog = ref(false);
const activeQuestionSessionId = ref('');
const pendingQuestionRequests = ref<QuestionRequest[]>([]);
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
const questionsAllDiffRoute = computed(() => ({
  path: '/diff',
  query: { sessionId: activeQuestionSessionId.value, mode: 'all' },
}));
const questionsDiffTarget = computed<DiffWorkspaceTarget>(() => ({
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

watch(questionsDialog, (open) => {
  if (!open) clearQuestionsContext();
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

function readSessionColumnWidths() {
  if (typeof window === 'undefined') return {} as Record<string, number>;
  try {
    const stored = JSON.parse(window.localStorage.getItem(horizontalWidthStorageKey) ?? '{}');
    if (!stored || typeof stored !== 'object' || Array.isArray(stored)) return {};
    return Object.fromEntries(
      Object.entries(stored).flatMap(([sessionId, width]) =>
        typeof width === 'number' && Number.isFinite(width) && width >= minSessionColumnWidth
          ? [[sessionId, Math.round(width)]]
          : [],
      ),
    );
  } catch {
    return {};
  }
}

function sessionColumnWidth(sessionId: string) {
  return (
    sessionColumnWidths.value[sessionId] ??
    Math.max(minSessionColumnWidth, Math.round($q.screen.width / 3))
  );
}

function setSessionColumnWidth(sessionId: string, width: number) {
  const nextWidth = Math.max(minSessionColumnWidth, Math.round(width));
  sessionColumnWidths.value = { ...sessionColumnWidths.value, [sessionId]: nextWidth };
  try {
    window.localStorage.setItem(
      horizontalWidthStorageKey,
      JSON.stringify(sessionColumnWidths.value),
    );
  } catch {
    // The resized columns remain usable when browser storage is unavailable.
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

function openNewSession() {
  if ($q.screen.lt.sm) {
    void router.push(
      projectScopeId.value
        ? { name: 'new-session', query: { projectId: projectScopeId.value } }
        : { name: 'new-session' },
    );
    return;
  }
  newSessionOpen.value = true;
}

function handleSessionUpdate(update: SessionUpdateEvent) {
  const index = latestRows.value.findIndex((card) => card.id === update.sessionId);
  if (index < 0) return;

  cardRefreshRequests.invalidate(update.sessionId);
  const status = update.status?.status;
  if (activeQuestionSessionId.value === update.sessionId && status && status !== 'waiting_user') {
    questionsDialog.value = false;
    clearQuestionsContext();
  }
  if (approvalSessionId.value === update.sessionId && status && status !== 'waiting_approval') {
    approvalDialog.value = false;
    clearApprovalContext();
  }
  if (status === 'closed') {
    latestRows.value = latestRows.value.filter((card) => card.id !== update.sessionId);
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
  if (card.availableActions.includes('retry_initialization')) {
    return { icon: 'refresh', color: 'primary', tooltip: '重试初始化', disabled: false };
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
    if (card.availableActions.includes('retry_initialization')) {
      await retrySessionInitialization(card.id);
    } else if (card.status === 'starting' || card.status === 'running') {
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

async function openQuestionsDialog(sessionId: string) {
  if ($q.screen.lt.sm) {
    await router.push({ name: 'session-detail', params: { id: sessionId } });
    return;
  }
  const requestGeneration = ++questionRequestGeneration;
  activeQuestionSessionId.value = sessionId;
  pendingQuestionRequests.value = [];
  questionsLoading.value = true;
  questionsDialog.value = true;
  try {
    const requests = await getPendingQuestionRequests(sessionId);
    if (
      requestGeneration === questionRequestGeneration &&
      activeQuestionSessionId.value === sessionId
    ) {
      const card = latestRows.value.find((item) => item.id === sessionId);
      if (card?.status !== 'waiting_user' || requests.length === 0) {
        questionsDialog.value = false;
        clearQuestionsContext();
        return;
      }
      pendingQuestionRequests.value = requests;
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

async function submitAnswers(requestId: string, answers: QuestionAnswerInput[]) {
  questionsSubmitting.value = true;
  try {
    await submitQuestionRequest(requestId, answers);
    questionsDialog.value = false;
  } finally {
    questionsSubmitting.value = false;
  }
}

async function openApprovalDialog(card: SessionCard) {
  if ($q.screen.lt.sm) {
    await router.push({ name: 'session-detail', params: { id: card.id } });
    return;
  }
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

function clearQuestionsContext() {
  questionRequestGeneration += 1;
  activeQuestionSessionId.value = '';
  pendingQuestionRequests.value = [];
  questionsLoading.value = false;
}

function isCurrentApprovalContext(requestGeneration: number, sessionId: string) {
  return approvalContextGeneration === requestGeneration && approvalSessionId.value === sessionId;
}

function openDiffDialog(card: SessionCard) {
  if ($q.screen.lt.sm) {
    void router.push({ path: '/diff', query: { sessionId: card.id, mode: 'all' } });
    return;
  }
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
  if ($q.screen.lt.sm) {
    void router.push({ name: 'session-artifacts', params: { id: card.id } });
    return;
  }
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
