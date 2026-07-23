import { createApp } from 'vue';
import { createPinia } from 'pinia';
import ECharts from 'vue-echarts';
import { use, registerTheme } from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { LineChart, GaugeChart, BarChart, PieChart, ScatterChart, HeatmapChart } from 'echarts/charts';
import {
  GridComponent, TooltipComponent, LegendComponent,
  DataZoomComponent, MarkLineComponent, TitleComponent,
  DatasetComponent, TransformComponent, VisualMapComponent,
} from 'echarts/components';

import App from './App.vue';
import router from './router';
import './assets/main.css';

// ECharts — register only what we need for minimal bundle size
use([
  CanvasRenderer,
  LineChart, GaugeChart, BarChart, PieChart, ScatterChart, HeatmapChart,
  GridComponent, TooltipComponent, LegendComponent,
  DataZoomComponent, MarkLineComponent, TitleComponent,
  DatasetComponent, TransformComponent, VisualMapComponent,
]);

// Dark theme for Ask Data charts — LLM-authored options carry no colors, so without
// this they'd render with ECharts' light defaults (near-invisible on the dark canvas).
// Palette mirrors the dashboard widgets (LineChartWidget.vue); fonts bumped for the
// large Ask Data canvas. containLabel keeps long axis labels from clipping.
registerTheme('cpf-dark', {
  textStyle: { color: '#d1d5db' },
  title: { textStyle: { color: '#f3f4f6' } },
  legend: { textStyle: { color: '#d1d5db' } },
  categoryAxis: { axisLabel: { color: '#9ca3af', fontSize: 12 }, axisLine: { lineStyle: { color: '#374151' } }, splitLine: { show: false } },
  valueAxis: { axisLabel: { color: '#9ca3af', fontSize: 12 }, splitLine: { lineStyle: { color: '#1f2937', type: 'dashed' } } },
  timeAxis: { axisLabel: { color: '#9ca3af', fontSize: 12 }, axisLine: { lineStyle: { color: '#374151' } } },
  grid: { containLabel: true },
});

const app = createApp(App);
const pinia = createPinia();

app.use(pinia);
app.use(router);
app.component('v-chart', ECharts);

// Mount only once the router has resolved the initial route, so the guard's
// redirects settle before first paint (no flash of a route we're leaving).
router.isReady().then(() => app.mount('#app'));
