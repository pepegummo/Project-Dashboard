import { defineStore } from 'pinia';
import { ref } from 'vue';

export const useWidgetViewStateStore = defineStore('widgetViewState', () => {
  const datetimeStates = ref<Record<string, { startDateTime: string; endDateTime: string }>>({});

  function setDatetime(widgetId: string, start: string, end: string) {
    datetimeStates.value[widgetId] = { startDateTime: start, endDateTime: end };
  }

  function remove(widgetId: string) {
    delete datetimeStates.value[widgetId];
  }

  return { datetimeStates, setDatetime, remove };
});
