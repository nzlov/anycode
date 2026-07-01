<template>
  <q-page class="page-shell detail-page">
    <div class="page-heading">
      <div>
        <div class="text-h5 text-weight-bold">{{ session?.title ?? '会话详情' }}</div>
        <div class="text-body2 text-muted">
          <template v-if="session">
            {{ session.projectId }} · {{ session.branch }} · {{ session.updatedAt }}
          </template>
          <template v-else>{{ sessionId }}</template>
        </div>
      </div>
      <div class="row q-gutter-sm">
        <q-btn outline color="primary" icon="difference" label="完整 Diff" no-caps to="/diff" />
        <q-btn
          v-if="canStop"
          unelevated
          color="negative"
          icon="stop"
          label="停止"
          no-caps
          :loading="stopping"
          @click="stopSession"
        />
      </div>
    </div>

    <div class="detail-grid">
      <section class="event-panel">
        <q-card flat bordered class="stream-card">
          <q-inner-loading :showing="loading">
            <q-spinner color="primary" size="32px" />
          </q-inner-loading>

          <q-banner v-if="error" dense class="bg-negative text-white">
            {{ error }}
          </q-banner>

          <q-card-section v-if="!loading && !error && events.length === 0" class="text-muted">
            暂无会话事件
          </q-card-section>

          <q-card-section v-for="event in events" :key="event.id" class="event-item">
            <div class="event-icon">
              <q-icon :name="eventIcon(event.kind)" />
            </div>
            <div class="event-body">
              <div class="row items-center q-gutter-sm">
                <div class="text-weight-medium">{{ event.title }}</div>
                <span class="text-caption text-muted">{{ event.time }}</span>
              </div>
              <div class="text-body2">{{ event.body }}</div>
            </div>
          </q-card-section>
        </q-card>

        <q-card flat bordered class="composer-card prompt-shell detail-composer">
          <q-input
            v-model.trim="appendText"
            autogrow
            borderless
            type="textarea"
            class="prompt-input"
            placeholder="追加描述，发送给当前会话"
            :disable="!session || appending || stopping"
          />
          <q-card-actions align="right">
            <q-btn
              unelevated
              color="primary"
              icon="send"
              label="发送"
              no-caps
              :loading="appending"
              :disable="!session || stopping || appendText.trim().length === 0"
              @click="sendAppend"
            />
          </q-card-actions>
        </q-card>
      </section>

      <aside class="right-panel">
        <q-card flat bordered>
          <q-tabs v-model="tab" dense align="justify" narrow-indicator>
            <q-tab name="info" icon="info" label="会话信息" />
            <q-tab name="changes" icon="difference" label="当前变更" />
          </q-tabs>
          <q-separator />
          <q-tab-panels v-model="tab" animated>
            <q-tab-panel name="info">
              <q-list separator>
                <q-item>
                  <q-item-section>
                    <q-item-label caption>模式</q-item-label>
                    <q-item-label>{{ session ? modeLabel(session.mode) : '-' }}</q-item-label>
                  </q-item-section>
                </q-item>
                <q-item>
                  <q-item-section>
                    <q-item-label caption>当前节点</q-item-label>
                    <q-item-label>{{ session?.node ?? '-' }}</q-item-label>
                  </q-item-section>
                </q-item>
                <q-item>
                  <q-item-section>
                    <q-item-label caption>状态</q-item-label>
                    <q-item-label>
                      <q-badge
                        v-if="session"
                        outline
                        :color="statusColor(session.status)"
                        :label="statusLabel(session.status)"
                      />
                      <template v-else>-</template>
                    </q-item-label>
                  </q-item-section>
                </q-item>
                <q-item>
                  <q-item-section>
                    <q-item-label caption>模型</q-item-label>
                    <q-item-label>{{ modelLabel }}</q-item-label>
                  </q-item-section>
                </q-item>
                <q-item>
                  <q-item-section>
                    <q-item-label caption>权限</q-item-label>
                    <q-item-label>{{ session?.config.permissionMode || '-' }}</q-item-label>
                  </q-item-section>
                </q-item>
                <q-item>
                  <q-item-section>
                    <q-item-label caption>思考强度</q-item-label>
                    <q-item-label>{{ session?.config.reasoningEffort || '-' }}</q-item-label>
                  </q-item-section>
                </q-item>
              </q-list>
            </q-tab-panel>

            <q-tab-panel name="changes">
              <q-banner dense class="bg-grey-2 text-grey-8">
                当前分支变更文件列表等待后端 Diff 接口接入；这里不展示 mock 数据。
              </q-banner>
              <q-btn
                class="full-width q-mt-md"
                outline
                color="primary"
                icon="open_in_new"
                label="查看全部"
                no-caps
                to="/diff"
              />
            </q-tab-panel>
          </q-tab-panels>
        </q-card>
      </aside>
    </div>
  </q-page>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { computed } from 'vue';
import { useRoute } from 'vue-router';

import { useSessionDetail } from '@/composables/useSessionDetail';
import type { SessionEvent, SessionMode, SessionStatus } from '@/services/sessions';

const route = useRoute();
const sessionId = String(route.params.id ?? '');
const appendText = ref('');
const tab = ref('info');
const {
  session,
  events,
  loading,
  appending,
  stopping,
  error,
  loadSessionDetail,
  appendDescription,
  stopSession,
} = useSessionDetail(sessionId);

const canStop = computed(() => session.value?.availableActions.includes('stop') ?? false);
const modelLabel = computed(() => session.value?.config.codexModel || 'Codex CLI 默认');

function eventIcon(kind: SessionEvent['kind']) {
  const icons: Record<SessionEvent['kind'], string> = {
    thought: 'psychology',
    tool: 'terminal',
    assistant: 'smart_toy',
    status: 'radio_button_checked',
    question: 'help',
  };
  return icons[kind];
}

function modeLabel(mode: SessionMode) {
  return mode === 'workflow' ? '流程模式' : '会话模式';
}

function statusColor(value: SessionStatus) {
  const colors: Record<SessionStatus, string> = {
    created: 'blue-grey',
    starting: 'primary',
    running: 'positive',
    waiting_user: 'warning',
    stopping: 'warning',
    stopped: 'blue-grey',
    resume_failed: 'negative',
    failed: 'negative',
    blocked: 'negative',
    completed: 'primary',
    closed: 'grey',
  };
  return colors[value];
}

function statusLabel(value: SessionStatus) {
  const labels: Record<SessionStatus, string> = {
    created: '待运行',
    starting: '启动中',
    running: '运行中',
    waiting_user: '待回答',
    stopping: '停止中',
    stopped: '已停止',
    resume_failed: '恢复失败',
    failed: '失败',
    blocked: '阻塞',
    completed: '已完成',
    closed: '已关闭',
  };
  return labels[value];
}

async function sendAppend() {
  const text = appendText.value;
  await appendDescription(text);
  appendText.value = '';
}

onMounted(() => {
  void loadSessionDetail();
});
</script>

<style scoped>
.detail-composer {
  grid-template-rows: minmax(120px, auto) auto;
}

.detail-composer :deep(.q-card__actions) {
  padding: 8px 12px 12px;
}
</style>
