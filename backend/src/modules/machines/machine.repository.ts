import { prisma } from '../../config/database';
import { Machine, MachineField, Prisma } from '@prisma/client';

export class MachineRepository {
  async findAll(organizationId: string, filters?: {
    productionLineId?: string;
    type?: string;
    status?: string;
  }) {
    return prisma.machine.findMany({
      where: {
        productionLine: { factory: { organizationId } },
        ...(filters?.productionLineId && { productionLineId: filters.productionLineId }),
        ...(filters?.type && { type: filters.type }),
        ...(filters?.status && { status: filters.status }),
      },
      include: {
        fields: { orderBy: { isKey: 'desc' } },
        productionLine: { include: { factory: true } },
        _count: { select: { alerts: true } },
      },
      orderBy: { name: 'asc' },
    });
  }

  async findById(id: string) {
    return prisma.machine.findUnique({
      where: { id },
      include: {
        fields: { orderBy: { isKey: 'desc' } },
        productionLine: { include: { factory: true } },
        alerts: { where: { isActive: true } },
      },
    });
  }

  async create(data: Prisma.MachineCreateInput) {
    return prisma.machine.create({ data, include: { fields: true } });
  }

  async update(id: string, data: Prisma.MachineUpdateInput) {
    return prisma.machine.update({ where: { id }, data, include: { fields: true } });
  }

  async updateStatus(id: string, status: string) {
    return prisma.machine.update({
      where: { id },
      data: { status, lastSeenAt: new Date() },
    });
  }

  async delete(id: string) {
    return prisma.machine.delete({ where: { id } });
  }

  // ─── Fields ───────────────────────────────────────────────────────────────
  async getFields(machineId: string): Promise<MachineField[]> {
    return prisma.machineField.findMany({
      where: { machineId },
      orderBy: [{ isKey: 'desc' }, { key: 'asc' }],
    });
  }

  async upsertField(machineId: string, field: {
    key: string;
    label: string;
    unit?: string;
    dataType?: string;
    min?: number;
    max?: number;
    isKey?: boolean;
  }) {
    return prisma.machineField.upsert({
      where: { machineId_key: { machineId, key: field.key } },
      update: field,
      create: { machineId, ...field },
    });
  }

  async deleteField(machineId: string, key: string) {
    return prisma.machineField.delete({
      where: { machineId_key: { machineId, key } },
    });
  }

  // ─── Factories / Lines ───────────────────────────────────────────────────
  async getProductionLines(organizationId: string) {
    return prisma.productionLine.findMany({
      where: { factory: { organizationId } },
      include: { factory: true, _count: { select: { machines: true } } },
      orderBy: { name: 'asc' },
    });
  }

  async getFactories(organizationId: string) {
    return prisma.factory.findMany({
      where: { organizationId },
      include: { _count: { select: { productionLines: true } } },
      orderBy: { name: 'asc' },
    });
  }
}
