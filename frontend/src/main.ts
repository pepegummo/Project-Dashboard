import { createApp } from 'vue';
import { createPinia } from 'pinia';
import ECharts from 'vue-echarts';
import { use } from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { LineChart, GaugeChart, BarChart } from 'echarts/charts';
import {
  GridComponent, TooltipComponent, LegendComponent,
  DataZoomComponent, MarkLineComponent, TitleComponent,
} from 'echarts/components';

import App from './App.vue';
import router from './router';
import './assets/main.css';

// ECharts — register only what we need for minimal bundle size
use([
  CanvasRenderer,
  LineChart, GaugeChart, BarChart,
  GridComponent, TooltipComponent, LegendComponent,
  DataZoomComponent, MarkLineComponent, TitleComponent,
]);

const app = createApp(App);
const pinia = createPinia();

app.use(pinia);
app.use(router);
app.component('v-chart', ECharts);

// Mount only once the router has resolved the initial route, so the guard's
// redirects settle before first paint (no flash of a route we're leaving).
router.isReady().then(() => app.mount('#app'));
