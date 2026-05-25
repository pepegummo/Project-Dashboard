import { Prisma } from '@prisma/client';
import { MachineRepository } from './machine.repository';
import { AppError } from '../../middleware/error';

export class MachineService {
  private repo = new MachineRepository();

  async getMachines(organizationId: string, filters?: Record<string, string>) {
    return this.repo.findAll(organizationId, filters);
  }

  async getMachineById(id: string, organizationId: string) {
    const machine = await this.repo.findById(id);
    if (!machine || machine.productionLine.factory.organizationId !== organizationId) {
      throw new AppError(404, 'NOT_FOUND', 'Machine not found');
    }
    return machine;
  }

  async createMachine(organizationId: string, data: {
    productionLineId: string;
    name: string;
    type: string;
    serialNumber?: string;
    model?: string;
    manufacturer?: string;
    metadata?: Record<string, unknown>;
    fields?: Array<{ key: string; label: string; unit?: string; dataType?: string; min?: number; max?: number; isKey?: boolean }>;
  }) {
    const { fields, ...machineData } = data;
    const machine = await this.repo.create({
      productionLine: { connect: { id: machineData.productionLineId } },
      name: machineData.name,
      type: machineData.type,
      serialNumber: machineData.serialNumber,
      model: machineData.model,
      manufacturer: machineData.manufacturer,
      metadata: (machineData.metadata ?? {}) as Prisma.InputJsonValue,
    });

    if (fields?.length) {
      for (const field of fields) {
        await this.repo.upsertField(machine.id, field);
      }
    }

    return this.repo.findById(machine.id);
  }

  async updateMachine(id: string, organizationId: string, data: Record<string, unknown>) {
    await this.getMachineById(id, organizationId); // auth check
    return this.repo.update(id, data as Prisma.MachineUpdateInput);
  }

  async deleteMachine(id: string, organizationId: string) {
    await this.getMachineById(id, organizationId); // auth check
    return this.repo.delete(id);
  }

  async getMachineFields(machineId: string, organizationId: string) {
    await this.getMachineById(machineId, organizationId); // auth check
    return this.repo.getFields(machineId);
  }

  async upsertMachineField(machineId: string, organizationId: string, field: {
    key: string;
    label: string;
    unit?: string;
    dataType?: string;
    min?: number;
    max?: number;
    isKey?: boolean;
  }) {
    await this.getMachineById(machineId, organizationId); // auth check
    return this.repo.upsertField(machineId, field);
  }

  async getProductionLines(organizationId: string) {
    return this.repo.getProductionLines(organizationId);
  }

  async getFactories(organizationId: string) {
    return this.repo.getFactories(organizationId);
  }
}
