<template>
  <q-page class="page-shell detail-page">
    <div class="page-heading">
      <div>
        <div class="text-h5 text-weight-bold">{{ session.title }}</div>
        <div class="text-body2 text-muted">
          {{ getProjectName(session.projectId) }} · {{ session.branch }} · {{ session.updatedAt }}
        </div>
      </div>
      <div class="row q-gutter-sm">
        <q-btn outline color="primary" icon="difference" label="完整 Diff" no-caps to="/diff" />
        <q-btn unelevated color="negative" icon="stop" label="停止" no-caps />
      </div>
    </div>

    <div class="detail-grid">
      <section class="event-panel">
        <q-card flat bordered class="stream-card">
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

        <q-card flat bordered class="composer-card">
          <q-input
            v-model.trim="appendText"
            autogrow
            borderless
            type="textarea"
            placeholder="追加描述，发送给当前会话"
          />
          <q-card-actions align="right">
            <q-btn
              unelevated
              color="primary"
              icon="send"
              label="发送"
              no-caps
              :loading="appending"
              :disable="appendText.trim().length === 0"
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
                    <q-item-label>{{
                      session.mode === 'workflow' ? '流程模式' : '会话模式'
                    }}</q-item-label>
                  </q-item-section>
                </q-item>
                <q-item>
                  <q-item-section>
                    <q-item-label caption>当前节点</q-item-label>
                    <q-item-label>{{ session.node }}</q-item-label>
                  </q-item-section>
                </q-item>
                <q-item>
                  <q-item-section>
                    <q-item-label caption>模型</q-item-label>
                    <q-item-label>Codex CLI 默认</q-item-label>
                  </q-item-section>
                </q-item>
                <q-item>
                  <q-item-section>
                    <q-item-label caption>权限</q-item-label>
                    <q-item-label>workspace-write</q-item-label>
                  </q-item-section>
                </q-item>
              </q-list>
            </q-tab-panel>

            <q-tab-panel name="changes">
              <q-list separator>
                <q-item v-for="file in diffFiles" :key="file.path" clickable>
                  <q-item-section avatar>
                    <q-icon :name="fileIcon(file.status)" :color="fileColor(file.status)" />
                  </q-item-section>
                  <q-item-section>
                    <q-item-label class="ellipsis">{{ file.path }}</q-item-label>
                    <q-item-label caption>
                      +{{ file.additions }} / -{{ file.deletions }}
                    </q-item-label>
                  </q-item-section>
                </q-item>
              </q-list>
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
import { useRoute } from 'vue-router';

import { useSessionDetail } from '@/composables/useSessionDetail';
import { diffFiles, getProjectName, type DiffFile, type SessionEvent } from '@/mocks/workbench';

const route = useRoute();
const sessionId = String(route.params.id ?? '');
const appendText = ref('');
const tab = ref('info');
const { session, events, appending, loadSessionDetail, appendDescription } =
  useSessionDetail(sessionId);

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

function fileIcon(status: DiffFile['status']) {
  return status === 'added' ? 'add_circle' : status === 'deleted' ? 'remove_circle' : 'edit';
}

function fileColor(status: DiffFile['status']) {
  return status === 'added' ? 'positive' : status === 'deleted' ? 'negative' : 'primary';
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
