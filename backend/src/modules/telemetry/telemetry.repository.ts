import { prisma } from '../../config/database';
import { TelemetryData } from '../../types';

export class TelemetryRepository {
  async ingest(machineId: string, data: TelemetryData, timestamp?: Date) {
    return prisma.telemetryRaw.create({
      data: {
        machineId,
        timestamp: timestamp ?? new Date(),
        data: data as any,
      },
    });
  }

  async getLatest(machineId: string): Promise<{ timestamp: Date; data: TelemetryData } | null> {
    const row = await prisma.telemetryRaw.findFirst({
      where: { machineId, timestamp: { lte: new Date() } },   // ← only rows up to NOW
      orderBy: { timestamp: 'desc' },
      select: { timestamp: true, data: true },
    });
    if (!row) return null;
    return { timestamp: row.timestamp, data: row.data as TelemetryData };
  }

  async getRange(machineId: string, from: Date, to: Date, limit = 1000) {
    return prisma.telemetryRaw.findMany({
      where: {
        machineId,
        timestamp: { gte: from, lte: to },
      },
      orderBy: { timestamp: 'asc' },
      take: limit,
      select: { timestamp: true, data: true },
    });
  }

  async getFieldSeries(machineId: string, field: string, from: Date, to: Date, limit = 500) {
    // Uses raw SQL for efficient field extraction from JSONB
    const rows = await prisma.$queryRaw<Array<{ ts: Date; value: number }>>`
      SELECT
        timestamp AS ts,
        (data->>${field})::float AS value
      FROM telemetry_raw
      WHERE machine_id = ${machineId}
        AND timestamp >= ${from}
        AND timestamp <= ${to}
        AND data ? ${field}
      ORDER BY timestamp ASC
      LIMIT ${limit}
    `;
    return rows;
  }

  async getTimescaleAggregate(machineId: string, field: string, from: Date, to: Date, bucket: string) {
    // TimescaleDB time_bucket for downsampled data
    try {
      const rows = await prisma.$queryRaw<Array<{
        bucket: Date; avg: number; min: number; max: number; count: bigint;
      }>>`
        SELECT
          time_bucket(${bucket}::interval, timestamp) AS bucket,
          AVG((data->>${field})::float) AS avg,
          MIN((data->>${field})::float) AS min,
          MAX((data->>${field})::float) AS max,
          COUNT(*) AS count
        FROM telemetry_raw
        WHERE machine_id = ${machineId}
          AND timestamp >= ${from}
          AND timestamp <= ${to}
          AND data ? ${field}
        GROUP BY bucket
        ORDER BY bucket ASC
      `;
      return rows.map(r => ({
        ...r,
        count: Number(r.count),
        avg: Number(r.avg),
        min: Number(r.min),
        max: Number(r.max),
      }));
    } catch {
      // Fallback if TimescaleDB not available
      return this.getFieldSeries(machineId, field, from, to);
    }
  }

  /** Single-row aggregate (avg / min / max) over an arbitrary time window */
  async getAggregateSummary(
    machineId: string,
    field: string,
    from: Date,
    to: Date,
  ): Promise<{ avg: number; min: number; max: number; count: number } | null> {
    try {
      const rows = await prisma.$queryRaw<Array<{
        avg: number | null;
        min: number | null;
        max: number | null;
        count: bigint;
      }>>`
        SELECT
          AVG((data->>${field})::float)  AS avg,
          MIN((data->>${field})::float)  AS min,
          MAX((data->>${field})::float)  AS max,
          COUNT(*)                        AS count
        FROM telemetry_raw
        WHERE machine_id = ${machineId}
          AND timestamp   >= ${from}
          AND timestamp   <= ${to}
          AND data ?       ${field}
      `;
      const row = rows[0];
      if (!row || Number(row.count) === 0) return null;
      return {
        avg:   Number(row.avg),
        min:   Number(row.min),
        max:   Number(row.max),
        count: Number(row.count),
      };
    } catch {
      return null;
    }
  }

  /** Count telemetry rows per day for N days back */
  async getDailyCount(machineId: string, days: number): Promise<Array<{ date: Date; count: number }>> {
    try {
      const from = new Date(Date.now() - days * 24 * 60 * 60 * 1000);
      const to   = new Date();
      const rows = await prisma.$queryRaw<Array<{ date: Date; count: bigint }>>`
        SELECT
          time_bucket('1 day', timestamp) AS date,
          COUNT(*) AS count
        FROM telemetry_raw
        WHERE machine_id = ${machineId}
          AND timestamp >= ${from}
          AND timestamp <= ${to}
        GROUP BY date
        ORDER BY date ASC
      `;
      return rows.map(r => ({ date: r.date, count: Number(r.count) }));
    } catch {
      return [];
    }
  }

  async getLatestForMachines(machineIds: string[]): Promise<Record<string, { timestamp: Date; data: TelemetryData }>> {
    if (!machineIds.length) return {};
    const rows = await prisma.$queryRaw<Array<{ machineId: string; timestamp: Date; data: any }>>`
      SELECT DISTINCT ON (machine_id)
        machine_id AS "machineId",
        timestamp,
        data
      FROM telemetry_raw
      WHERE machine_id = ANY(${machineIds}::uuid[])
        AND timestamp <= NOW()
      ORDER BY machine_id, timestamp DESC
    `;
    return Object.fromEntries(rows.map(r => [r.machineId, { timestamp: r.timestamp, data: r.data }]));
  }
}
