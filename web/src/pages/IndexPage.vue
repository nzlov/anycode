<template>
  <q-page class="workbench-page q-pa-md q-pa-lg-xl">
    <div class="row items-center q-mb-lg">
      <div class="col">
        <div class="text-h5 text-weight-bold">总揽</div>
        <div class="text-body2 text-muted">最近 3 天运行卡片与近 7 天历史记录</div>
      </div>
      <q-btn flat color="primary" icon-right="chevron_right" label="更多历史" no-caps />
    </div>

    <div class="row q-col-gutter-lg">
      <section class="col-12 col-lg-7">
        <div class="row items-center q-mb-sm">
          <div class="text-subtitle1 text-weight-bold">最近</div>
          <q-space />
          <q-chip dense outline color="primary" icon="schedule">3 天内</q-chip>
        </div>
        <div class="column q-gutter-md">
          <q-card v-for="card in recentCards" :key="card.title" class="session-card">
            <q-card-section>
              <div class="row items-start q-col-gutter-md">
                <div class="col">
                  <div class="row items-center q-gutter-sm q-mb-xs">
                    <q-badge :color="card.running ? 'positive' : 'blue-grey'" rounded />
                    <span class="text-weight-bold">{{ card.title }}</span>
                    <q-badge outline color="primary" :label="card.mode" />
                  </div>
                  <div class="text-body2 text-muted">{{ card.summary }}</div>
                  <q-separator class="q-my-sm" />
                  <div class="row items-center q-gutter-sm text-caption text-muted">
                    <q-icon name="account_tree" />
                    <span>{{ card.branch }}</span>
                    <q-icon name="radio_button_checked" />
                    <span>{{ card.node }}</span>
                  </div>
                </div>
                <q-btn flat round color="primary" icon="chevron_right" aria-label="打开卡片" />
              </div>
            </q-card-section>
          </q-card>
        </div>
      </section>

      <section class="col-12 col-lg-5">
        <div class="row items-center q-mb-sm">
          <div class="text-subtitle1 text-weight-bold">历史</div>
          <q-space />
          <q-chip dense outline color="secondary" icon="history">近 7 天</q-chip>
        </div>
        <q-list bordered separator class="bg-white rounded-borders">
          <q-item v-for="item in historyCards" :key="item.title" clickable>
            <q-item-section avatar>
              <q-icon
                :name="item.running ? 'play_arrow' : 'stop_circle'"
                :color="item.running ? 'positive' : 'blue-grey'"
              />
            </q-item-section>
            <q-item-section>
              <q-item-label>{{ item.title }}</q-item-label>
              <q-item-label caption>{{ item.project }} · {{ item.updatedAt }}</q-item-label>
            </q-item-section>
            <q-item-section side>
              <q-badge
                outline
                :color="item.pending ? 'warning' : 'blue-grey'"
                :label="item.pending ? '待回答' : '已同步'"
              />
            </q-item-section>
          </q-item>
        </q-list>
      </section>
    </div>

    <q-page-sticky position="bottom-right" :offset="[24, 24]">
      <q-fab color="positive" icon="add" direction="up" aria-label="新建卡片" />
    </q-page-sticky>
  </q-page>
</template>

<script setup lang="ts">
const recentCards = [
  {
    title: '实现 answer_user 选项回答',
    summary: '会话详情页展示事件流，右侧显示会话信息和当前分支变更。',
    running: true,
    mode: '流程模式',
    branch: 'feature/answer-user',
    node: '验证构建结果',
  },
  {
    title: '生成 Turso 迁移风险计划',
    summary: '分析 ent schema 与 libSQL 连接策略，输出可验证 TODO。',
    running: false,
    mode: '会话模式',
    branch: 'main',
    node: '已停止',
  },
];

const historyCards = [
  {
    title: 'OpenCode runtime 替换为 Codex runtime',
    project: 'openchamber',
    updatedAt: '今天 09:42',
    running: true,
    pending: false,
  },
  {
    title: 'questionbank 增加去重导入检查',
    project: 'pets',
    updatedAt: '昨天 18:10',
    running: false,
    pending: true,
  },
  {
    title: 'Turso 迁移风险并生成计划',
    project: 'anycode',
    updatedAt: '2026-06-29',
    running: false,
    pending: false,
  },
];
</script>
