<template>
  <q-card flat bordered class="statistics-chart">
    <q-card-section class="statistics-chart__header">
      <div>
        <div class="statistics-chart__title">{{ title }}</div>
        <div class="statistics-chart__caption">{{ caption }}</div>
      </div>
      <div v-if="series.length" class="statistics-chart__legend" aria-label="图例">
        <span v-for="item in series" :key="item.key" class="statistics-chart__legend-item">
          <span class="statistics-chart__swatch" :style="{ backgroundColor: item.color }" />
          {{ item.label }}
        </span>
      </div>
    </q-card-section>

    <q-separator />

    <div
      v-if="labels.length && series.length"
      ref="viewport"
      class="statistics-chart__viewport"
      @scroll.passive="handleViewportScroll"
    >
      <svg
        class="statistics-chart__svg"
        :style="{ width: `${chartWidth}px` }"
        :viewBox="`0 0 ${chartWidth} ${chartHeight}`"
        role="img"
        :aria-label="`${title}，${caption}`"
      >
        <g class="statistics-chart__grid">
          <template v-for="tick in ticks" :key="tick.ratio">
            <line
              :x1="padding.left"
              :x2="chartWidth - padding.right"
              :y1="tickY(tick.ratio)"
              :y2="tickY(tick.ratio)"
            />
            <text :x="padding.left - 8" :y="tickY(tick.ratio) + 4" text-anchor="end">
              {{ formatCompact(tick.value) }}
            </text>
          </template>
        </g>

        <template v-if="kind === 'bar'">
          <g v-for="(item, seriesIndex) in series" :key="item.key">
            <rect
              v-for="(value, index) in item.values"
              :key="`${item.key}-${keys[index]}`"
              :x="barX(index, seriesIndex)"
              :y="valueY(value)"
              :width="barWidth"
              :height="barHeight(value)"
              :fill="item.color"
              rx="2"
            >
              <title>{{ labels[index] }} · {{ item.label }}：{{ formatNumber(value) }}</title>
            </rect>
          </g>
        </template>

        <template v-else>
          <g v-for="item in series" :key="item.key">
            <polyline
              class="statistics-chart__line"
              :points="linePoints(item.values)"
              :stroke="item.color"
            />
            <circle
              v-for="(value, index) in item.values"
              :key="`${item.key}-${keys[index]}`"
              :cx="xCenter(index)"
              :cy="valueY(value)"
              r="4"
              :fill="item.color"
            >
              <title>{{ labels[index] }} · {{ item.label }}：{{ formatNumber(value) }}</title>
            </circle>
          </g>
        </template>

        <g class="statistics-chart__labels">
          <text
            v-for="(label, index) in labels"
            :key="keys[index]"
            :x="xCenter(index)"
            :y="chartHeight - 18"
            text-anchor="middle"
          >
            {{ shortLabel(label) }}
            <title>{{ label }}</title>
          </text>
        </g>
      </svg>
    </div>
    <div v-else class="statistics-chart__empty">暂无统计数据</div>
  </q-card>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue';

export interface StatisticsChartSeries {
  key: string;
  label: string;
  color: string;
  values: number[];
}

const props = withDefaults(
  defineProps<{
    title: string;
    caption: string;
    keys: string[];
    labels: string[];
    series: StatisticsChartSeries[];
    scrollLeft?: number;
    kind?: 'bar' | 'line';
  }>(),
  { kind: 'bar', scrollLeft: 0 },
);
const emit = defineEmits<{ viewportScroll: [value: number] }>();
const viewport = ref<HTMLElement | null>(null);

const chartHeight = 260;
const padding = { top: 20, right: 20, bottom: 54, left: 56 };
const chartWidth = computed(() =>
  Math.max(620, props.labels.length * 72 + padding.left + padding.right),
);
const plotHeight = chartHeight - padding.top - padding.bottom;
const plotWidth = computed(() => chartWidth.value - padding.left - padding.right);
const maximumValue = computed(() => {
  const maximum = Math.max(0, ...props.series.flatMap((item) => item.values));
  return maximum > 0 ? maximum : 0;
});
const tickStep = computed(() => niceStep(maximumValue.value / 4));
const chartMaximum = computed(() => tickStep.value * 4);
const ticks = computed(() =>
  [4, 3, 2, 1, 0].map((step) => ({ ratio: step / 4, value: tickStep.value * step })),
);
const groupWidth = computed(() => plotWidth.value / Math.max(props.labels.length, 1));
const barWidth = computed(() =>
  Math.max(4, Math.min(24, (groupWidth.value * 0.7) / Math.max(props.series.length, 1))),
);

function syncScrollLeft(value: number) {
  void nextTick(() => {
    if (viewport.value && Math.abs(viewport.value.scrollLeft - value) > 0.5) {
      viewport.value.scrollLeft = value;
    }
  });
}

function handleViewportScroll() {
  if (viewport.value) emit('viewportScroll', viewport.value.scrollLeft);
}

function tickY(ratio: number) {
  return padding.top + plotHeight * (1 - ratio);
}

function xCenter(index: number) {
  return padding.left + groupWidth.value * (index + 0.5);
}

function valueY(value: number) {
  return padding.top + plotHeight * (1 - Math.max(0, value) / chartMaximum.value);
}

function barHeight(value: number) {
  return Math.max(0, chartHeight - padding.bottom - valueY(value));
}

function barX(index: number, seriesIndex: number) {
  const totalWidth = barWidth.value * props.series.length;
  return xCenter(index) - totalWidth / 2 + seriesIndex * barWidth.value;
}

function linePoints(values: number[]) {
  return values.map((value, index) => `${xCenter(index)},${valueY(value)}`).join(' ');
}

function shortLabel(value: string) {
  return value.length > 12 ? `${value.slice(0, 11)}…` : value;
}

function formatNumber(value: number) {
  return new Intl.NumberFormat('zh-CN').format(value);
}

function formatCompact(value: number) {
  return new Intl.NumberFormat('zh-CN', { notation: 'compact', maximumFractionDigits: 1 }).format(
    value,
  );
}

function niceStep(raw: number) {
  if (raw <= 1) return 1;
  const magnitude = 10 ** Math.floor(Math.log10(raw));
  const normalized = raw / magnitude;
  if (normalized <= 1) return magnitude;
  if (normalized <= 2) return magnitude * 2;
  if (normalized <= 5) return magnitude * 5;
  return magnitude * 10;
}

watch(() => props.scrollLeft, syncScrollLeft);
onMounted(() => syncScrollLeft(props.scrollLeft));
</script>

<style scoped>
.statistics-chart {
  min-width: 0;
  overflow: hidden;
  background: var(--ac-surface);
  border-color: var(--ac-border);
  border-radius: var(--ac-radius);
}

.statistics-chart__header {
  display: flex;
  min-height: 72px;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.statistics-chart__title {
  font-size: 16px;
  font-weight: 600;
}

.statistics-chart__caption {
  margin-top: 2px;
  color: var(--ac-text-muted);
  font-size: 12px;
}

.statistics-chart__legend {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 12px;
  color: var(--ac-text-muted);
  font-size: 12px;
}

.statistics-chart__legend-item {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  white-space: nowrap;
}

.statistics-chart__swatch {
  width: 10px;
  height: 10px;
  border-radius: 2px;
}

.statistics-chart__viewport {
  overflow-x: auto;
  overflow-y: hidden;
}

.statistics-chart__svg {
  display: block;
  min-width: 100%;
  height: 260px;
}

.statistics-chart__grid line {
  stroke: var(--ac-border);
  stroke-width: 1;
}

.statistics-chart__grid text,
.statistics-chart__labels text {
  fill: var(--ac-text-muted);
  font-size: 11px;
}

.statistics-chart__line {
  fill: none;
  stroke-width: 3;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.statistics-chart__empty {
  display: grid;
  min-height: 260px;
  place-items: center;
  color: var(--ac-text-muted);
}

@media (max-width: 599.98px) {
  .statistics-chart__header {
    min-height: 64px;
    padding: 12px;
  }
}
</style>
