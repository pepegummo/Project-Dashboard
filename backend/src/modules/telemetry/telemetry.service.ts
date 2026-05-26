import { TelemetryRepository } from './telemetry.repository';
import { MachineRepository } from '../machines/machine.repository';
import { AppError } from '../../middleware/error';
import { TelemetryData } from '../../types';

// Data is stored at 1-minute resolution — buckets below 1m are clamped to '1 minute'
const TIME_RANGE_PRESETS: Record<string, number> = {
  '5m':  5   * 60 * 1000,
  '15m': 15  * 60 * 1000,
  '30m': 30  * 60 * 1000,
  '1h':  60  * 60 * 1000,
  '6h':  6   * 60 * 60 * 1000,
  '24h': 24  * 60 * 60 * 1000,
  '7d':  7   * 24 * 60 * 60 * 1000,
  '15d': 15  * 24 * 60 * 60 * 1000,
  '30d': 30  * 24 * 60 * 60 * 1000,
  '3mo': 90  * 24 * 60 * 60 * 1000,
  '6mo': 180 * 24 * 60 * 60 * 1000,
  '1y':  365 * 24 * 60 * 60 * 1000,
};

// TimescaleDB time_bucket size per range.
// Rule: bucket MUST be < 1 pulse cycle (120 min) to preserve pulse shape in AVG.
// For ranges where that would give >1500 pts we accept a flat avg but compensate
// with min/max band in the frontend (see LineChartWidget).
// Pulse cycle = 120 min (2 h). Keep bucket < 60 min to ensure the bucket never
// spans a full cycle (which would cause avg ≈ threshold → flat line).
// For ranges where that gives too many points use ≥ 1-cycle buckets + min/max band.
const BUCKET_FOR_RANGE: Record<string, string> = {
  '5m':  '1 minute',   //    5 pts  — raw
  '15m': '1 minute',   //   15 pts  — raw
  '30m': '1 minute',   //   30 pts  — raw
  '1h':  '1 minute',   //   60 pts  — raw
  '6h':  '5 minutes',  //   72 pts  — sub-cycle → pulse visible
  '24h': '15 minutes', //   96 pts  — sub-cycle → pulse visible
  '7d':  '30 minutes', //  336 pts  — sub-cycle → pulse visible
  '15d': '30 minutes', //  720 pts  — sub-cycle → pulse visible (was 1h → hit cycle boundary)
  '30d': '1 hour',     //  720 pts  — half-cycle → pulse visible
  '3mo': '1 hour',     // 2160 pts  — half-cycle → pulse visible
  '6mo': '1 hour',     // 4320 pts  — half-cycle → pulse visible
  '1y':  '1 hour',     // 8760 pts  — half-cycle → pulse visible
};

export class TelemetryService {
  private repo = new TelemetryRepository();
  private machineRepo = new MachineRepository();

  async ingest(machineId: string, data: TelemetryData, organizationId: string) {
    const machine = await this.machineRepo.findById(machineId);
    if (!machine || machine.productionLine.factory.organizationId !== organizationId) {
      throw new AppError(404, 'NOT_FOUND', 'Machine not found');
    }
    await this.repo.ingest(machineId, data);
    await this.machineRepo.updateStatus(machineId, 'online');
    return { machineId, timestamp: new Date(), data };
  }

  async getLatest(machineId: string, organizationId: string) {
    const machine = await this.machineRepo.findById(machineId);
    if (!machine || machine.productionLine.factory.organizationId !== organizationId) {
      throw new AppError(404, 'NOT_FOUND', 'Machine not found');
    }
    return this.repo.getLatest(machineId);
  }

  async getSeries(machineId: string, field: string, timeRange: string, organizationId: string) {
    const machine = await this.machineRepo.findById(machineId);
    if (!machine || machine.productionLine.factory.organizationId !== organizationId) {
      throw new AppError(404, 'NOT_FOUND', 'Machine not found');
    }

    const rangeMs = TIME_RANGE_PRESETS[timeRange] ?? TIME_RANGE_PRESETS['1h'];
    const to = new Date();
    const from = new Date(to.getTime() - rangeMs);
    const bucket = BUCKET_FOR_RANGE[timeRange] ?? '1 minute';

    const data = await this.repo.getTimescaleAggregate(machineId, field, from, to, bucket);
    return { machineId, field, timeRange, from, to, data };
  }

  /** Single aggregated value (avg/min/max) for a field over a look-back period */
  async getAggregate(machineId: string, field: string, period: string, organizationId: string) {
    const machine = await this.machineRepo.findById(machineId);
    if (!machine || machine.productionLine.factory.organizationId !== organizationId) {
      throw new AppError(404, 'NOT_FOUND', 'Machine not found');
    }
    const rangeMs = TIME_RANGE_PRESETS[period] ?? TIME_RANGE_PRESETS['1h'];
    const to   = new Date();
    const from = new Date(to.getTime() - rangeMs);
    const summary = await this.repo.getAggregateSummary(machineId, field, from, to);
    return { machineId, field, period, from, to, summary };
  }

  async getDailyCount(machineId: string, days: number, organizationId: string) {
    const machine = await this.machineRepo.findById(machineId);
    if (!machine || machine.productionLine.factory.organizationId !== organizationId) {
      throw new AppError(404, 'NOT_FOUND', 'Machine not found');
    }
    const data = await this.repo.getDailyCount(machineId, days);
    return { machineId, days, data };
  }

  async getMultiMachineLatest(machineIds: string[], organizationId: string) {
    // Filter to org-owned machines only
    const allMachines = await this.machineRepo.findAll(organizationId);
    const ownedIds = new Set(allMachines.map(m => m.id));
    const filteredIds = machineIds.filter(id => ownedIds.has(id));
    return this.repo.getLatestForMachines(filteredIds);
  }
}
