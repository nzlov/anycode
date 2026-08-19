<template>
  <q-page class="surface-page surface-page--wide statistics-page">
    <PageToolbar title="统计" />

    <section class="statistics-summary" aria-label="今日与总计">
      <q-card
        v-for="item in summaryItems"
        :key="item.key"
        flat
        bordered
        class="statistics-summary__card"
      >
        <q-card-section>
          <div class="statistics-summary__label">
            <q-icon :name="item.icon" size="20px" />
            {{ item.label }}
          </div>
          <div class="statistics-summary__today">
            <span>今日</span>
            <strong>{{ formatSummaryValue(item.key, item.today) }}</strong>
          </div>
          <div class="statistics-summary__total">
            总计 {{ formatSummaryValue(item.key, item.total) }}
          </div>
        </q-card-section>
      </q-card>
    </section>

    <section class="statistics-controls" aria-label="统计时间范围">
      <span class="statistics-controls__label">时间范围：</span>
      <q-btn-dropdown
        v-model="rangeMenuOpen"
        outline
        no-caps
        icon="date_range"
        :label="rangeLabel"
        class="statistics-range-button"
        :disable="loading"
      >
        <div class="statistics-range-menu">
          <div class="statistics-range-presets">
            <q-btn
              v-for="preset in rangePresets"
              :key="preset.value"
              v-close-popup
              flat
              no-caps
              :label="preset.label"
              :class="{ 'statistics-range-preset--active': activePreset === preset.value }"
              @click="selectPreset(preset.value)"
            />
          </div>
          <q-separator />
          <div class="statistics-custom-range">
            <q-input
              v-model="customStartDate"
              outlined
              dense
              stack-label
              type="date"
              label="开始日期"
            />
            <q-icon name="arrow_forward" size="20px" class="statistics-custom-range__arrow" />
            <q-input
              v-model="customEndDate"
              outlined
              dense
              stack-label
              type="date"
              label="结束日期"
            />
            <q-btn
              v-close-popup
              unelevated
              no-caps
              color="primary"
              label="应用"
              class="statistics-custom-range__apply"
              :disable="!customRangeValid || loading"
              @click="applyCustomRange"
            />
          </div>
        </div>
      </q-btn-dropdown>
      <q-btn outline no-caps icon="refresh" label="刷新" :loading="loading" @click="load" />
    </section>

    <q-banner v-if="error" rounded class="statistics-error">
      {{ error }}
      <template #action>
        <q-btn flat color="primary" label="重试" @click="load" />
      </template>
    </q-banner>

    <section v-else class="statistics-charts" :aria-busy="loading">
      <StatisticsChart
        title="创建卡片"
        :caption="createdCaption"
        :keys="chartKeys"
        :labels="chartLabels"
        :series="createdSeries"
        :scroll-left="chartScrollLeft"
        kind="line"
        @viewport-scroll="syncChartScroll"
      />
      <StatisticsChart
        title="关闭卡片"
        :caption="closedCaption"
        :keys="chartKeys"
        :labels="chartLabels"
        :series="closedSeries"
        :scroll-left="chartScrollLeft"
        kind="line"
        @viewport-scroll="syncChartScroll"
      />
      <StatisticsChart
        title="修改文件"
        :caption="fileCaption"
        :keys="chartKeys"
        :labels="chartLabels"
        :series="fileSeries"
        :scroll-left="chartScrollLeft"
        kind="line"
        @viewport-scroll="syncChartScroll"
      />
      <StatisticsChart
        title="Token 用量"
        :caption="tokenCaption"
        :keys="chartKeys"
        :labels="chartLabels"
        :series="tokenSeries"
        :scroll-left="chartScrollLeft"
        kind="line"
        @viewport-scroll="syncChartScroll"
      />
      <q-inner-loading :showing="loading">
        <q-spinner color="primary" size="36px" />
      </q-inner-loading>
    </section>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';

import PageToolbar from '@/components/PageToolbar.vue';
import StatisticsChart, { type StatisticsChartSeries } from '@/components/StatisticsChart.vue';
import {
  getStatistics,
  type StatisticsDashboard,
  type StatisticsMetrics,
  type StatisticsRange,
  type StatisticsTimelineBucket,
} from '@/services/statistics';
import { formatTokenCount } from '@/services/tokenUsagePresentation';

type MetricKey = keyof StatisticsMetrics;
type RangePreset = 'last7' | 'last15' | 'thisMonth' | 'lastMonth' | 'custom';

const emptyDashboard: StatisticsDashboard = {
  today: { createdCards: 0, closedCards: 0, filesChanged: 0, totalTokens: 0 },
  total: { createdCards: 0, closedCards: 0, filesChanged: 0, totalTokens: 0 },
  byDay: [],
};
const rangePresets: Array<{ label: string; value: Exclude<RangePreset, 'custom'> }> = [
  { label: '近 7 天', value: 'last7' },
  { label: '近 15 天', value: 'last15' },
  { label: '本月', value: 'thisMonth' },
  { label: '上月', value: 'lastMonth' },
];
const initialRange = presetDateRange('last7');
const dashboard = ref<StatisticsDashboard>(emptyDashboard);
const loading = ref(false);
const error = ref('');
const rangeMenuOpen = ref(false);
const activePreset = ref<RangePreset>('last7');
const selectedRange = ref<StatisticsRange>(initialRange);
const customStartDate = ref(initialRange.startDate);
const customEndDate = ref(initialRange.endDate);
const chartScrollLeft = ref(0);
const buckets = computed<StatisticsTimelineBucket[]>(() => {
  const archived = new Map(dashboard.value.byDay.map((bucket) => [bucket.key, bucket]));
  const result: StatisticsTimelineBucket[] = [];
  const end = parseLocalDate(selectedRange.value.endDate);
  for (
    let current = parseLocalDate(selectedRange.value.startDate);
    current <= end;
    current = addDays(current, 1)
  ) {
    const key = formatLocalDate(current);
    result.push(archived.get(key) ?? { key, label: key.slice(5), projects: [] });
  }
  return result;
});
const chartKeys = computed(() => buckets.value.map((bucket) => bucket.key));
const chartLabels = computed(() => buckets.value.map((bucket) => bucket.label));
const rangeLabel = computed(() => {
  return rangePresets.find((preset) => preset.value === activePreset.value)?.label ?? '自定义';
});
const customRangeValid = computed(
  () =>
    Boolean(customStartDate.value && customEndDate.value) &&
    customStartDate.value <= customEndDate.value,
);
const createdCaption = '每日按项目统计创建数量';
const closedCaption = '每日按项目统计关闭数量';
const fileCaption = '每日按项目汇总卡片最后一次 Diff 快照';
const tokenCaption = '每日按项目统计实际 Token 增量';
const summaryItems = computed(() => [
  {
    key: 'created',
    label: '创建卡片',
    icon: 'add_task',
    today: dashboard.value.today.createdCards,
    total: dashboard.value.total.createdCards,
  },
  {
    key: 'closed',
    label: '关闭卡片',
    icon: 'task_alt',
    today: dashboard.value.today.closedCards,
    total: dashboard.value.total.closedCards,
  },
  {
    key: 'files',
    label: '修改文件',
    icon: 'difference',
    today: dashboard.value.today.filesChanged,
    total: dashboard.value.total.filesChanged,
  },
  {
    key: 'tokens',
    label: 'Token 用量',
    icon: 'data_usage',
    today: dashboard.value.today.totalTokens,
    total: dashboard.value.total.totalTokens,
  },
]);
const projectColors = [
  'var(--ac-primary)',
  'var(--ac-secondary)',
  'var(--ac-tertiary)',
  'var(--ac-error)',
  'var(--ac-on-primary-fixed-variant)',
  'var(--ac-on-secondary-fixed-variant)',
  'var(--ac-on-tertiary-fixed-variant)',
  'var(--ac-outline)',
];
const projects = computed(() => {
  const values = new Map<string, string>();
  for (const bucket of buckets.value) {
    for (const project of bucket.projects) values.set(project.key, project.label);
  }
  return [...values]
    .map(([key, label]) => ({ key, label }))
    .sort((left, right) => {
      const labelOrder = left.label.localeCompare(right.label, 'zh-CN');
      return labelOrder || left.key.localeCompare(right.key);
    });
});

function buildProjectSeries(metric: MetricKey): StatisticsChartSeries[] {
  return projects.value.map((project, index) => ({
    key: project.key,
    label: project.label,
    color: projectColors[index % projectColors.length]!,
    values: buckets.value.map(
      (bucket) => bucket.projects.find((item) => item.key === project.key)?.metrics[metric] ?? 0,
    ),
  }));
}

const createdSeries = computed(() => buildProjectSeries('createdCards'));
const closedSeries = computed(() => buildProjectSeries('closedCards'));
const fileSeries = computed(() => buildProjectSeries('filesChanged'));
const tokenSeries = computed(() => buildProjectSeries('totalTokens'));

async function load() {
  if (loading.value) return;
  loading.value = true;
  error.value = '';
  try {
    dashboard.value = await getStatistics(selectedRange.value);
  } catch (loadError) {
    error.value = loadError instanceof Error ? loadError.message : '统计加载失败';
  } finally {
    loading.value = false;
  }
}

function selectPreset(preset: Exclude<RangePreset, 'custom'>) {
  const dateRange = presetDateRange(preset);
  activePreset.value = preset;
  selectedRange.value = dateRange;
  customStartDate.value = dateRange.startDate;
  customEndDate.value = dateRange.endDate;
  rangeMenuOpen.value = false;
  chartScrollLeft.value = 0;
  void load();
}

function applyCustomRange() {
  if (!customRangeValid.value) return;
  activePreset.value = 'custom';
  selectedRange.value = { startDate: customStartDate.value, endDate: customEndDate.value };
  rangeMenuOpen.value = false;
  chartScrollLeft.value = 0;
  void load();
}

function syncChartScroll(value: number) {
  chartScrollLeft.value = value;
}

function presetDateRange(preset: Exclude<RangePreset, 'custom'>): StatisticsRange {
  const today = new Date();
  const endDate = formatLocalDate(today);
  if (preset === 'last7' || preset === 'last15') {
    const days = preset === 'last7' ? 7 : 15;
    return { startDate: formatLocalDate(addDays(today, 1 - days)), endDate };
  }
  if (preset === 'thisMonth') {
    return {
      startDate: formatLocalDate(new Date(today.getFullYear(), today.getMonth(), 1)),
      endDate,
    };
  }
  return {
    startDate: formatLocalDate(new Date(today.getFullYear(), today.getMonth() - 1, 1)),
    endDate: formatLocalDate(new Date(today.getFullYear(), today.getMonth(), 0)),
  };
}

function parseLocalDate(value: string) {
  return new Date(
    Number(value.slice(0, 4)),
    Number(value.slice(5, 7)) - 1,
    Number(value.slice(8, 10)),
  );
}

function addDays(value: Date, days: number) {
  return new Date(value.getFullYear(), value.getMonth(), value.getDate() + days);
}

function formatLocalDate(value: Date) {
  return [
    String(value.getFullYear()).padStart(4, '0'),
    String(value.getMonth() + 1).padStart(2, '0'),
    String(value.getDate()).padStart(2, '0'),
  ].join('-');
}

function formatNumber(value: number) {
  return new Intl.NumberFormat('zh-CN').format(value);
}

function formatSummaryValue(key: string, value: number) {
  return key === 'tokens' ? formatTokenCount(value) : formatNumber(value);
}

onMounted(load);
</script>

<style scoped>
.statistics-page {
  padding-bottom: 32px;
}

.statistics-summary {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

.statistics-summary__card {
  min-width: 0;
  background: var(--ac-surface);
  border-color: var(--ac-border);
  border-radius: var(--ac-radius);
}

.statistics-summary__label {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 8px;
  color: var(--ac-text-muted);
  font-size: 13px;
}

.statistics-summary__today {
  display: flex;
  min-width: 0;
  align-items: baseline;
  gap: 8px;
  margin-top: 12px;
  overflow: hidden;
}

.statistics-summary__today span {
  flex: 0 0 auto;
  color: var(--ac-text-muted);
  font-size: 12px;
  font-weight: 400;
}

.statistics-summary__today strong {
  min-width: 0;
  overflow: hidden;
  font-size: 28px;
  font-weight: 700;
  line-height: 1.1;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.statistics-summary__total {
  margin-top: 6px;
  overflow: hidden;
  color: var(--ac-text-muted);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.statistics-controls {
  display: flex;
  min-height: 64px;
  align-items: center;
  gap: 10px;
  margin: 20px 0 12px;
  padding: 10px 14px;
  background: var(--ac-surface);
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
}

.statistics-controls__label {
  flex: 0 0 auto;
  color: var(--ac-text-muted);
}

.statistics-range-button {
  min-width: 156px;
}

.statistics-range-menu {
  width: min(460px, calc(100vw - 24px));
  background: var(--ac-surface-raised);
}

.statistics-range-presets {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 4px;
  padding: 10px;
}

.statistics-range-preset--active {
  background: var(--ac-surface-selected);
  color: var(--ac-text);
}

.statistics-custom-range {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr) auto;
  align-items: end;
  gap: 10px;
  padding: 14px;
}

.statistics-custom-range__arrow {
  align-self: center;
  color: var(--ac-text-muted);
}

.statistics-charts {
  position: relative;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.statistics-error {
  background: var(--ac-status-danger-bg);
  color: var(--ac-status-danger-text);
}

@media (max-width: 899.98px) {
  .statistics-summary {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .statistics-charts {
    grid-template-columns: minmax(0, 1fr);
  }
}

@media (max-width: 599.98px) {
  .statistics-page {
    padding: 8px;
  }

  .statistics-summary {
    gap: 8px;
  }

  .statistics-summary__card .q-card__section {
    padding: 12px;
  }

  .statistics-summary__today strong {
    font-size: 24px;
  }

  .statistics-controls {
    margin-top: 12px;
  }

  .statistics-controls__label {
    display: none;
  }

  .statistics-range-button {
    min-width: 0;
  }

  .statistics-custom-range {
    grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
  }

  .statistics-custom-range__apply {
    grid-column: 1 / -1;
    justify-self: end;
  }

  .statistics-charts {
    gap: 8px;
  }
}
</style>
