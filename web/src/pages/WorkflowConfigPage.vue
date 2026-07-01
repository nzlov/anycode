<template>
  <q-page class="page-shell">
    <div class="page-heading">
      <div>
        <div class="text-h5 text-weight-bold">流程配置</div>
        <div class="text-body2 text-muted">{{ projectName }} · JSON AST 条件分支与人工审批入口</div>
      </div>
      <q-btn outline color="primary" icon="save" label="保存流程" no-caps />
    </div>

    <div class="workflow-layout">
      <q-card flat bordered class="workflow-list">
        <q-card-section class="row items-center">
          <div class="text-subtitle1 text-weight-bold">节点</div>
          <q-space />
          <q-btn flat round dense icon="add" color="primary" aria-label="新增节点" />
        </q-card-section>
        <q-list separator>
          <q-item v-for="node in nodes" :key="node.id" clickable :active="node.id === selectedNode">
            <q-item-section avatar>
              <q-icon :name="node.icon" color="primary" />
            </q-item-section>
            <q-item-section>
              <q-item-label>{{ node.title }}</q-item-label>
              <q-item-label caption>{{ node.caption }}</q-item-label>
            </q-item-section>
          </q-item>
        </q-list>
      </q-card>

      <q-card flat bordered class="workflow-canvas">
        <q-card-section class="canvas-grid">
          <div v-for="node in nodes" :key="node.id" class="workflow-node">
            <q-icon :name="node.icon" color="primary" />
            <div>
              <div class="text-weight-medium">{{ node.title }}</div>
              <div class="text-caption text-muted">{{ node.caption }}</div>
            </div>
          </div>
        </q-card-section>
      </q-card>

      <q-card flat bordered class="workflow-editor">
        <q-card-section>
          <div class="text-subtitle1 text-weight-bold">节点配置</div>
        </q-card-section>
        <q-card-section class="q-gutter-md">
          <q-input v-model="nodeTitle" dense outlined label="标题" />
          <q-input v-model="nodePrompt" autogrow outlined type="textarea" label="提示词" />
          <q-input v-model.number="retry" dense outlined type="number" label="失败重试次数" />
          <q-toggle v-model="requiresApproval" label="需要人工审批" />
          <q-select
            v-model="condition"
            dense
            outlined
            label="出边条件"
            :options="conditionOptions"
          />
        </q-card-section>
      </q-card>
    </div>
  </q-page>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';
import { useRoute } from 'vue-router';

import { getProjectName } from '@/mocks/workbench';

const route = useRoute();
const selectedNode = ref('plan');
const nodeTitle = ref('实现节点');
const nodePrompt = ref('按当前任务目标执行，并在失败时输出结构化错误。');
const retry = ref(1);
const requiresApproval = ref(false);
const condition = ref('status == succeeded');

const projectName = computed(() => getProjectName(String(route.params.projectId ?? 'anycode')));

const nodes = [
  { id: 'plan', title: '计划', caption: '整理 TODO 与验证方式', icon: 'fact_check' },
  { id: 'implement', title: '实现', caption: '运行 Codex 进程', icon: 'terminal' },
  { id: 'review', title: '人工审批', caption: '等待用户确认', icon: 'approval' },
  { id: 'verify', title: '验证', caption: '执行 lint/build/test', icon: 'task_alt' },
];

const conditionOptions = [
  'status == succeeded',
  'has_pending_question',
  'files_changed > 0',
  'not(resume_failed)',
];
</script>
