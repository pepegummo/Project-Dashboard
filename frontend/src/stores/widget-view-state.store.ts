import { defineStore } from 'pinia';
import { ref } from 'vue';

export interface WidgetSeries {
  columns: string[];
  data: (string | number)[][];
}

export const useWidgetViewStateStore = defineStore('widgetViewState', () => {
  const datetimeStates = ref<Record<string, { startDateTime: string; endDateTime: string }>>({});
  const bucketStates = ref<Record<string, string>>({});
  // Compacted snapshot of the data a chart/count widget is actually rendering,
  // so the AI can read exactly what's on screen (focused widget only) without a tool call.
  const seriesStates = ref<Record<string, WidgetSeries>>({});
  // Last widget element the user clicked (chart point, title, axis, unit, value, legend,
  // threshold), so the AI page can mention it + inject a one-line context hint. nonce forces
  // the page's watch to fire again even when the same element is clicked twice in a row.
  const lastElementClick = ref<{
    widgetId: string;
    title: string;
    element: 'point' | 'title' | 'x-axis' | 'y-axis' | 'unit' | 'value' | 'legend' | 'threshold';
    seriesName?: string;
    x?: string | number;
    xLabel?: string;
    value?: string | number;
    detail?: string;
    nonce: number;
  } | null>(null);

  // Gates all element-click tracking (canvas region classification + HTML delegation) so
  // every other page (editor, LED, dashboard) keeps its pristine click behavior — only the
  // AI page flips this on (mount → unmount).
  const elementPickMode = ref(false);

  function setDatetime(widgetId: string, start: string, end: string) {
    datetimeStates.value[widgetId] = { startDateTime: start, endDateTime: end };
  }

  function setBucket(widgetId: string, bucket: string) {
    bucketStates.value[widgetId] = bucket;
  }

  function setSeries(widgetId: string, series: WidgetSeries) {
    seriesStates.value[widgetId] = series;
  }

  function setElementClick(p: Omit<NonNullable<typeof lastElementClick.value>, 'nonce'>) {
    lastElementClick.value = { ...p, nonce: Date.now() };
  }

  function setElementPickMode(on: boolean) {
    elementPickMode.value = on;
  }

  function remove(widgetId: string) {
    delete datetimeStates.value[widgetId];
    delete bucketStates.value[widgetId];
    delete seriesStates.value[widgetId];
  }

  return {
    datetimeStates, bucketStates, seriesStates, lastElementClick, elementPickMode,
    setDatetime, setBucket, setSeries, setElementClick, setElementPickMode, remove,
  };
});
