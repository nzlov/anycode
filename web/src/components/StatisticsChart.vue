<template>
  <q-card flat bordered class="statistics-chart">
    <q-card-section class="statistics-chart__header">
      <div>
        <div class="statistics-chart__title">{{ title }}</div>
        <div class="statistics-chart__caption">{{ caption }}</div>
      </div>
      <div v-if="series.length" class="statistics-chart__legend" aria-label="图例">
        <button
          v-for="item in series"
          :key="item.key"
          type="button"
          class="statistics-chart__legend-item"
          :class="{ 'statistics-chart__legend-item--hidden': hiddenSeriesKeys.has(item.key) }"
          :aria-pressed="!hiddenSeriesKeys.has(item.key)"
          :aria-label="`${hiddenSeriesKeys.has(item.key) ? '显示' : '隐藏'}${item.label}曲线`"
          @click="toggleSeries(item.key)"
        >
          <span class="statistics-chart__swatch" :style="{ backgroundColor: item.color }" />
          {{ item.label }}
          <q-icon
            :name="hiddenSeriesKeys.has(item.key) ? 'visibility_off' : 'visibility'"
            size="14px"
          />
        </button>
      </div>
    </q-card-section>

    <q-separator />

    <div v-if="labels.length && series.length" class="statistics-chart__plot">
      <div ref="viewport" class="statistics-chart__viewport" @scroll.passive="handleViewportScroll">
        <svg
          class="statistics-chart__svg"
          :style="{ width: `${chartWidth}px` }"
          :viewBox="`0 0 ${chartWidth} ${chartHeight}`"
          role="img"
          :aria-label="`${title}，${caption}`"
          @mousemove="handleMouseMove"
          @mouseleave="clearInspection"
          @touchstart.passive="handleTouch"
          @touchmove.passive="handleTouch"
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
                {{ formatAxisValue(tick.value) }}
              </text>
            </template>
          </g>

          <template v-if="kind === 'bar'">
            <g v-for="(item, seriesIndex) in visibleSeries" :key="item.key">
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
                <title>{{ labels[index] }} · {{ item.label }}：{{ formatValue(value) }}</title>
              </rect>
            </g>
          </template>

          <template v-else>
            <g v-for="item in visibleSeries" :key="item.key">
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
                <title>{{ labels[index] }} · {{ item.label }}：{{ formatValue(value) }}</title>
              </circle>
            </g>
          </template>

          <g v-if="activeData" class="statistics-chart__marker" aria-hidden="true">
            <line
              :x1="xCenter(activeData.index)"
              :x2="xCenter(activeData.index)"
              :y1="padding.top"
              :y2="chartHeight - padding.bottom"
            />
            <circle
              v-for="item in activeData.items"
              :key="item.key"
              :cx="xCenter(activeData.index)"
              :cy="valueY(item.value)"
              r="6"
              :fill="item.color"
            />
          </g>

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
      <div
        v-if="activeData"
        class="statistics-chart__inspection"
        :class="{
          'statistics-chart__inspection--after': activeRatio !== null && activeRatio < 0.4,
          'statistics-chart__inspection--before': activeRatio !== null && activeRatio > 0.6,
        }"
        :style="inspectionStyle"
        aria-live="polite"
        :aria-label="`${activeData.label} 的具体数据`"
      >
        <strong>{{ activeData.label }}</strong>
        <span v-for="item in activeData.items" :key="item.key">
          <i :style="{ backgroundColor: item.color }" />
          {{ item.label }} {{ formatValue(item.value) }}
        </span>
      </div>
    </div>
    <div v-else class="statistics-chart__empty">暂无统计数据</div>
  </q-card>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue';

import { formatTokenCount } from '@/services/tokenUsagePresentation';

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
    valueFormat?: 'number' | 'tokens';
    activeIndex?: number | null;
    activeRatio?: number | null;
  }>(),
  { kind: 'bar', scrollLeft: 0, valueFormat: 'number', activeIndex: null, activeRatio: null },
);
const emit = defineEmits<{
  viewportScroll: [value: number];
  inspect: [index: number | null, ratio: number | null];
}>();
const viewport = ref<HTMLElement | null>(null);
const hiddenSeriesKeys = ref(new Set<string>());

const chartHeight = 260;
const padding = { top: 20, right: 20, bottom: 54, left: 56 };
const chartWidth = computed(() =>
  Math.max(620, props.labels.length * 72 + padding.left + padding.right),
);
const plotHeight = chartHeight - padding.top - padding.bottom;
const plotWidth = computed(() => chartWidth.value - padding.left - padding.right);
const visibleSeries = computed(() =>
  props.series.filter((item) => !hiddenSeriesKeys.value.has(item.key)),
);
const maximumValue = computed(() => {
  const maximum = Math.max(0, ...visibleSeries.value.flatMap((item) => item.values));
  return maximum > 0 ? maximum : 0;
});
const tickStep = computed(() => niceStep(maximumValue.value / 4));
const chartMaximum = computed(() => tickStep.value * 4);
const ticks = computed(() =>
  [4, 3, 2, 1, 0].map((step) => ({ ratio: step / 4, value: tickStep.value * step })),
);
const groupWidth = computed(() => plotWidth.value / Math.max(props.labels.length, 1));
const barWidth = computed(() =>
  Math.max(4, Math.min(24, (groupWidth.value * 0.7) / Math.max(visibleSeries.value.length, 1))),
);
const activeData = computed(() => {
  const index = props.activeIndex;
  if (index === null || index === undefined || index < 0 || index >= props.labels.length) {
    return null;
  }
  return {
    index,
    label: props.keys[index] ?? props.labels[index]!,
    items: visibleSeries.value.map((item) => ({
      key: item.key,
      label: item.label,
      color: item.color,
      value: item.values[index] ?? 0,
    })),
  };
});
const activeRatio = computed(() => {
  if (props.activeRatio === null || props.activeRatio === undefined) return null;
  return Math.max(0, Math.min(1, props.activeRatio));
});
const inspectionStyle = computed(() => {
  if (activeRatio.value === null) return undefined;
  return { left: `${activeRatio.value * 100}%` };
});

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

function toggleSeries(key: string) {
  const next = new Set(hiddenSeriesKeys.value);
  if (next.has(key)) next.delete(key);
  else next.add(key);
  hiddenSeriesKeys.value = next;
}

function inspectAtClientPosition(clientX: number, clientY: number, target: SVGSVGElement) {
  const bounds = target.getBoundingClientRect();
  if (bounds.width <= 0) return;
  const chartX = ((clientX - bounds.left) / bounds.width) * chartWidth.value;
  const chartY = ((clientY - bounds.top) / bounds.height) * chartHeight;
  if (
    chartX < padding.left ||
    chartX > chartWidth.value - padding.right ||
    chartY < padding.top ||
    chartY > chartHeight - padding.bottom
  ) {
    clearInspection();
    return;
  }
  const ratio = (chartX - padding.left) / plotWidth.value;
  const index = Math.max(
    0,
    Math.min(props.labels.length - 1, Math.round(ratio * props.labels.length - 0.5)),
  );
  const viewportBounds = viewport.value?.getBoundingClientRect();
  const pointerRatio = viewportBounds
    ? Math.max(0, Math.min(1, (clientX - viewportBounds.left) / viewportBounds.width))
    : ratio;
  emit('inspect', index, pointerRatio);
}

function handleMouseMove(event: MouseEvent) {
  inspectAtClientPosition(event.clientX, event.clientY, event.currentTarget as SVGSVGElement);
}

function handleTouch(event: TouchEvent) {
  const touch = event.touches[0];
  if (touch) {
    inspectAtClientPosition(touch.clientX, touch.clientY, event.currentTarget as SVGSVGElement);
  }
}

function clearInspection() {
  emit('inspect', null, null);
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
  const totalWidth = barWidth.value * visibleSeries.value.length;
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

function formatValue(value: number) {
  return props.valueFormat === 'tokens' ? formatTokenCount(value) : formatNumber(value);
}

function formatAxisValue(value: number) {
  if (props.valueFormat === 'tokens') return formatTokenCount(value);
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
watch(
  () => props.keys,
  () => {
    hiddenSeriesKeys.value = new Set();
  },
);
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
  appearance: none;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 0;
  background: transparent;
  border: 0;
  color: inherit;
  cursor: pointer;
  font: inherit;
  white-space: nowrap;
}

.statistics-chart__legend-item:hover {
  color: var(--ac-text);
}

.statistics-chart__legend-item:focus-visible {
  outline: 2px solid var(--ac-primary);
  outline-offset: 3px;
}

.statistics-chart__legend-item--hidden {
  opacity: 0.5;
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

.statistics-chart__plot {
  position: relative;
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

.statistics-chart__marker {
  pointer-events: none;
}

.statistics-chart__marker line {
  stroke: var(--ac-text);
  stroke-dasharray: 4 4;
  stroke-width: 1;
}

.statistics-chart__marker circle {
  stroke: var(--ac-surface);
  stroke-width: 3;
}

.statistics-chart__inspection {
  position: absolute;
  z-index: 1;
  top: 10px;
  display: flex;
  width: max-content;
  max-width: min(320px, calc(100% - 24px));
  flex-wrap: wrap;
  align-items: center;
  gap: 4px 12px;
  padding: 8px 10px;
  background: var(--ac-surface-raised);
  border: 1px solid var(--ac-border);
  border-radius: 6px;
  box-shadow: var(--ac-shadow-card);
  color: var(--ac-text);
  font-size: 12px;
  line-height: 1.4;
  pointer-events: none;
  transform: translateX(-50%);
}

.statistics-chart__inspection--after {
  transform: translateX(10px);
}

.statistics-chart__inspection--before {
  transform: translateX(calc(-100% - 10px));
}

.statistics-chart__inspection strong {
  flex-basis: 100%;
}

.statistics-chart__inspection span {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  white-space: nowrap;
}

.statistics-chart__inspection i {
  width: 8px;
  height: 8px;
  flex: 0 0 auto;
  border-radius: 2px;
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
