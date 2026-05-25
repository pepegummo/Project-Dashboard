import { defineStore } from 'pinia';
import { ref, reactive } from 'vue';
import type { TelemetrySnapshot, TelemetryValue } from '@/types';

const MAX_HISTORY = 300; // max data points per field per machine

export interface TelemetryHistory {
  timestamps: string[];
  values: number[];
}

export const useTelemetryStore = defineStore('telemetry', () => {
  // Latest snapshot per machine
  const snapshots = reactive<Record<string, TelemetrySnapshot>>({});

  // Rolling history per machine per field: { machineId: { field: { timestamps, values } } }
  const history = reactive<Record<string, Record<string, TelemetryHistory>>>({});

  function updateSnapshot(machineId: string, timestamp: string, data: Record<string, TelemetryValue>) {
    snapshots[machineId] = { timestamp, data };

    // Append to rolling history
    if (!history[machineId]) history[machineId] = {};
    for (const [field, rawValue] of Object.entries(data)) {
      if (typeof rawValue !== 'number') continue;
      if (!history[machineId][field]) {
        history[machineId][field] = { timestamps: [], values: [] };
      }
      const h = history[machineId][field];
      h.timestamps.push(timestamp);
      h.values.push(rawValue);
      // Trim to max size
      if (h.timestamps.length > MAX_HISTORY) {
        h.timestamps.shift();
        h.values.shift();
      }
    }
  }

  function getLatest(machineId: string): TelemetrySnapshot | undefined {
    return snapshots[machineId];
  }

  function getFieldValue(machineId: string, field: string): number | undefined {
    const snap = snapshots[machineId];
    if (!snap) return undefined;
    const v = snap.data[field];
    return typeof v === 'number' ? v : undefined;
  }

  function getHistory(machineId: string, field: string): TelemetryHistory | undefined {
    return history[machineId]?.[field];
  }

  function clearHistory(machineId?: string) {
    if (machineId) {
      delete history[machineId];
    } else {
      Object.keys(history).forEach(k => delete history[k]);
    }
  }

  return { snapshots, history, updateSnapshot, getLatest, getFieldValue, getHistory, clearHistory };
});
