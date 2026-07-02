/**
 * mergeSeries — align several single-field telemetry series onto one shared,
 * sorted time axis. Pure (no vue/api imports) so it has a runnable self-check
 * in mergeSeries.selftest.ts. Buckets already align across fields (same machine +
 * range → same bucket size), but timestamps present in one field and not another
 * are gap-filled with null; ECharts renders those as line breaks / missing bars.
 */
export interface FieldPoints {
  field: string;
  points: Array<{ ts: string; value: number | null }>;
}

export interface MergedSeries {
  categories: string[];
  series: Array<{ field: string; data: Array<number | null> }>;
}

export function mergeSeries(perField: FieldPoints[]): MergedSeries {
  // Union of all timestamps, sorted chronologically (ISO strings sort correctly).
  const tsSet = new Set<string>();
  for (const f of perField) for (const p of f.points) tsSet.add(p.ts);
  const categories = [...tsSet].sort();

  const index = new Map(categories.map((ts, i) => [ts, i]));

  const series = perField.map(f => {
    const data: Array<number | null> = new Array(categories.length).fill(null);
    for (const p of f.points) {
      const i = index.get(p.ts);
      if (i !== undefined) data[i] = p.value;
    }
    return { field: f.field, data };
  });

  return { categories, series };
}
