import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const pageSource = readSource('../src/pages/StatisticsPage.vue');
const chartSource = readSource('../src/components/StatisticsChart.vue');
const serviceSource = readSource('../src/services/statistics.ts');
const layoutSource = readSource('../src/layouts/MainLayout.vue');
const routesSource = readSource('../src/router/routes.ts');
const projectsPageSource = readSource('../src/pages/ProjectsPage.vue');
const appStylesSource = readSource('../src/css/app.scss');

test('project list and statistics share the expanded desktop page width', () => {
  assert.match(projectsPageSource, /surface-page surface-page--expanded project-list-page/);
  assert.match(pageSource, /surface-page surface-page--expanded statistics-page/);
  assert.match(appStylesSource, /\.surface-page--expanded\s*{[^}]*max-width:\s*1920px/s);
});

test('statistics page exposes today totals and project series over time', () => {
  assert.match(pageSource, /title="统计"/);
  assert.doesNotMatch(pageSource, /<PageToolbar[^>]*title-icon=/);
  assert.match(pageSource, /v-for="item in summaryItems"/);
  assert.match(pageSource, /formatSummaryValue\(item\.key, item\.today\)/);
  assert.match(pageSource, /formatSummaryValue\(item\.key, item\.total\)/);
  assert.match(pageSource, /key === 'tokens' \? formatTokenCount\(value\) : formatNumber\(value\)/);
  assert.match(pageSource, /today: dashboard\.value\.today\.createdCards/);
  assert.match(pageSource, /total: dashboard\.value\.total\.totalTokens/);
  assert.match(pageSource, /label: '近 7 天'/);
  assert.match(pageSource, /label: '近 15 天'/);
  assert.match(pageSource, /label: '本月'/);
  assert.match(pageSource, /label: '上月'/);
  assert.match(pageSource, /label="开始日期"/);
  assert.match(pageSource, /label="结束日期"/);
  assert.doesNotMatch(pageSource, /label: '按月'/);
  assert.doesNotMatch(pageSource, /label: '按项目'/);
  assert.match(pageSource, /title="创建卡片"/);
  assert.match(pageSource, /title="关闭卡片"/);
  assert.match(pageSource, /title="修改文件"/);
  assert.match(pageSource, /title="Token 用量"/);
  assert.match(pageSource, /buildProjectSeries\('createdCards'\)/);
  assert.match(pageSource, /buildProjectSeries\('closedCards'\)/);
  assert.match(pageSource, /bucket\.projects\.find/);
});

test('statistics query reads the requested daily range from the server-side archive', () => {
  assert.match(serviceSource, /query Statistics\(\$input: StatisticsQueryInput!\)/);
  assert.match(serviceSource, /statistics\(input: \$input\)/);
  assert.match(serviceSource, /variables: \{ input: range \}/);
  assert.doesNotMatch(serviceSource, /utcOffsetMinutes/);
  assert.match(serviceSource, /byDay/);
  assert.doesNotMatch(serviceSource, /byMonth/);
  assert.doesNotMatch(serviceSource, /byProject/);
  assert.match(serviceSource, /projects \{/);
  assert.match(serviceSource, /interface StatisticsProjectMetrics/);
});

test('statistics charts remain inspectable with long timelines and project names', () => {
  assert.match(chartSource, /overflow-x: auto/);
  assert.match(chartSource, /Math\.max\(620, props\.labels\.length \* 72/);
  assert.match(chartSource, /<title>\{\{ labels\[index\] \}\}/);
  assert.match(chartSource, /shortLabel\(label\)/);
  assert.match(chartSource, /v-if="series\.length"/);
  assert.match(chartSource, /@scroll\.passive="handleViewportScroll"/);
  assert.match(chartSource, /emit\('viewportScroll', viewport\.value\.scrollLeft\)/);
  assert.match(pageSource, /:scroll-left="chartScrollLeft"/);
  assert.match(pageSource, /@viewport-scroll="syncChartScroll"/);
});

test('statistics charts share token units and linked pointer inspection', () => {
  assert.match(pageSource, /:active-index="chartActiveIndex"/);
  assert.match(pageSource, /:active-ratio="chartInspectionRatio"/);
  assert.match(pageSource, /@inspect="syncChartInspection"/);
  assert.match(pageSource, /value-format="tokens"/);
  assert.match(chartSource, /import \{ formatTokenCount \}/);
  assert.match(chartSource, /props\.valueFormat === 'tokens' \? formatTokenCount\(value\)/);
  assert.match(chartSource, /@mousemove="handleMouseMove"/);
  assert.match(chartSource, /@touchstart\.passive="handleTouch"/);
  assert.match(chartSource, /@touchmove\.passive="handleTouch"/);
  assert.match(chartSource, /class="statistics-chart__inspection"/);
  assert.match(chartSource, /label: props\.keys\[index\] \?\? props\.labels\[index\]!/);
  assert.match(chartSource, /chartY < padding\.top/);
  assert.match(chartSource, /chartY > chartHeight - padding\.bottom/);
  assert.match(chartSource, /emit\('inspect', index, pointerRatio\)/);
  assert.match(chartSource, /left: `\$\{activeRatio\.value \* 100\}%`/);
});

test('statistics charts only show changed projects and allow toggling series', () => {
  assert.match(pageSource, /values\.some\(\(value\) => value > 0\)/);
  assert.match(chartSource, /const visibleSeries = computed/);
  assert.match(chartSource, /<button[\s\S]*?@click="toggleSeries\(item\.key\)"/);
  assert.match(chartSource, /:aria-pressed="!hiddenSeriesKeys\.has\(item\.key\)"/);
  assert.match(chartSource, /v-for="item in visibleSeries"/);
  assert.match(chartSource, /visibleSeries\.value\.flatMap/);
});

test('overview toolbar and router expose statistics navigation', () => {
  assert.match(layoutSource, /icon="analytics"/);
  assert.match(layoutSource, /aria-label="统计"/);
  assert.match(layoutSource, /:to="\{ name: 'statistics' \}"/);
  assert.match(routesSource, /path: 'statistics',[\s\S]*?name: 'statistics'/);
});
