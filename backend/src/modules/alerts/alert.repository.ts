import { prisma } from '../../config/database';
import { Prisma } from '@prisma/client';

export class AlertRepository {
  async findAll(organizationId: string, machineId?: string) {
    return prisma.alert.findMany({
      where: {
        machine: { productionLine: { factory: { organizationId } } },
        ...(machineId && { machineId }),
      },
      include: {
        machine: { select: { id: true, name: true, type: true } },
        _count: { select: { events: { where: { status: 'open' } } } },
      },
      orderBy: { createdAt: 'desc' },
    });
  }

  async findById(id: string) {
    return prisma.alert.findUnique({
      where: { id },
      include: {
        machine: { include: { productionLine: { include: { factory: true } } } },
        events: { orderBy: { createdAt: 'desc' }, take: 50 },
      },
    });
  }

  async create(data: Prisma.AlertCreateInput) {
    return prisma.alert.create({ data, include: { machine: true } });
  }

  async update(id: string, data: Prisma.AlertUpdateInput) {
    return prisma.alert.update({ where: { id }, data, include: { machine: true } });
  }

  async delete(id: string) {
    return prisma.alert.delete({ where: { id } });
  }

  async getActiveAlerts(organizationId: string | null, limit = 50) {
    return prisma.alertEvent.findMany({
      where: {
        status: 'open',
        alert: {
          isActive: true,
          ...(organizationId
            ? { machine: { productionLine: { factory: { organizationId } } } }
            : {}),
        },
      },
      include: {
        alert: {
          include: { machine: { select: { id: true, name: true, type: true } } },
        },
      },
      orderBy: { createdAt: 'desc' },
      take: limit,
    });
  }

  async createEvent(data: {
    alertId: string;
    value: number;
    message?: string;
  }) {
    return prisma.alertEvent.create({ data });
  }

  async acknowledgeEvent(eventId: string, userId: string) {
    return prisma.alertEvent.update({
      where: { id: eventId },
      data: { status: 'acknowledged', resolvedBy: userId },
    });
  }

  async resolveEvent(eventId: string, userId: string) {
    return prisma.alertEvent.update({
      where: { id: eventId },
      data: { status: 'resolved', resolvedAt: new Date(), resolvedBy: userId },
    });
  }

  async getAlertsForMachines(machineIds: string[]) {
    return prisma.alert.findMany({
      where: { machineId: { in: machineIds }, isActive: true },
    });
  }
}
