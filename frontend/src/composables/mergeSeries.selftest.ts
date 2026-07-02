/**
 * Runnable self-check for mergeSeries. No test runner in this project, so run:
 *   npx --yes tsx frontend/src/composables/mergeSeries.selftest.ts
 * Exits non-zero (throws) if the merge logic breaks.
 */
import assert from 'node:assert/strict';
import { mergeSeries } from './mergeSeries';

// Two fields with partly-overlapping, out-of-order timestamps.
const merged = mergeSeries([
  { field: 'speed', points: [{ ts: '2026-01-01T02:00', value: 20 }, { ts: '2026-01-01T00:00', value: 10 }] },
  { field: 'temp',  points: [{ ts: '2026-01-01T01:00', value: 55 }, { ts: '2026-01-01T00:00', value: 50 }] },
]);

// Union of timestamps, sorted chronologically.
assert.deepEqual(merged.categories, ['2026-01-01T00:00', '2026-01-01T01:00', '2026-01-01T02:00']);
// speed missing the 01:00 bucket → null; temp missing 02:00 → null.
assert.deepEqual(merged.series[0], { field: 'speed', data: [10, null, 20] });
assert.deepEqual(merged.series[1], { field: 'temp',  data: [50, 55, null] });

// Empty input is safe.
assert.deepEqual(mergeSeries([]), { categories: [], series: [] });

console.log('mergeSeries self-check passed');
