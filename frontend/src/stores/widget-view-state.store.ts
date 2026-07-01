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

  function setDatetime(widgetId: string, start: string, end: string) {
    datetimeStates.value[widgetId] = { startDateTime: start, endDateTime: end };
  }

  function setBucket(widgetId: string, bucket: string) {
    bucketStates.value[widgetId] = bucket;
  }

  function setSeries(widgetId: string, series: WidgetSeries) {
    seriesStates.value[widgetId] = series;
  }

  function remove(widgetId: string) {
    delete datetimeStates.value[widgetId];
    delete bucketStates.value[widgetId];
    delete seriesStates.value[widgetId];
  }

  return { datetimeStates, bucketStates, seriesStates, setDatetime, setBucket, setSeries, remove };
});
