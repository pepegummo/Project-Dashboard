import { TelemetryRepository } from './telemetry.repository';
import { MachineRepository } from '../machines/machine.repository';
import { AppError } from '../../middleware/error';
import { TelemetryData } from '../../types';

const TIME_RANGE_PRESETS: Record<string, number> = {
  '5m': 5 * 60 * 1000,
  '15m': 15 * 60 * 1000,
  '30m': 30 * 60 * 1000,
  '1h': 60 * 60 * 1000,
  '6h': 6 * 60 * 60 * 1000,
  '24h': 24 * 60 * 60 * 1000,
  '7d': 7 * 24 * 60 * 60 * 1000,
};

const BUCKET_FOR_RANGE: Record<string, string> = {
  '5m': '10 seconds',
  '15m': '30 seconds',
  '30m': '1 minute',
  '1h': '1 minute',
  '6h': '5 minutes',
  '24h': '15 minutes',
  '7d': '1 hour',
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

  async getMultiMachineLatest(machineIds: string[], organizationId: string) {
    // Filter to org-owned machines only
    const allMachines = await this.machineRepo.findAll(organizationId);
    const ownedIds = new Set(allMachines.map(m => m.id));
    const filteredIds = machineIds.filter(id => ownedIds.has(id));
    return this.repo.getLatestForMachines(filteredIds);
  }
}
