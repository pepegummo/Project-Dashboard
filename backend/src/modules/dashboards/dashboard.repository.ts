import { prisma } from '../../config/database';
import { Prisma } from '@prisma/client';

export class DashboardRepository {
  async findAll(organizationId: string, userId?: string) {
    return prisma.dashboard.findMany({
      where: {
        organizationId,
        ...(userId && { OR: [{ userId }, { isPublic: true }] }),
      },
      include: {
        user: { select: { id: true, name: true, email: true } },
        _count: { select: { widgets: true } },
      },
      orderBy: [{ isDefault: 'desc' }, { updatedAt: 'desc' }],
    });
  }

  async findById(id: string) {
    return prisma.dashboard.findUnique({
      where: { id },
      include: {
        widgets: {
          include: { machine: { select: { id: true, name: true, type: true, fields: true } } },
          orderBy: { order: 'asc' },
        },
        user: { select: { id: true, name: true } },
      },
    });
  }

  async create(data: Prisma.DashboardCreateInput) {
    return prisma.dashboard.create({ data });
  }

  async update(id: string, data: Prisma.DashboardUpdateInput) {
    return prisma.dashboard.update({ where: { id }, data });
  }

  async delete(id: string) {
    return prisma.dashboard.delete({ where: { id } });
  }

  // ─── Widgets ──────────────────────────────────────────────────────────────
  async addWidget(dashboardId: string, widget: {
    machineId?: string;
    widgetType: string;
    title?: string;
    layout: object;
    config: object;
    order?: number;
  }) {
    return prisma.dashboardWidget.create({
      data: {
        dashboardId,
        ...widget,
        layout: widget.layout as Prisma.InputJsonValue,
        config: widget.config as Prisma.InputJsonValue,
      },
      include: { machine: { select: { id: true, name: true, type: true, fields: true } } },
    });
  }

  async updateWidget(widgetId: string, data: {
    machineId?: string | null;
    title?: string;
    layout?: object;
    config?: object;
  }) {
    return prisma.dashboardWidget.update({
      where: { id: widgetId },
      data: {
        ...(data.machineId !== undefined && {
          machine: data.machineId
            ? { connect: { id: data.machineId } }
            : { disconnect: true },
        }),
        ...(data.title !== undefined && { title: data.title }),
        ...(data.layout && { layout: data.layout as Prisma.InputJsonValue }),
        ...(data.config && { config: data.config as Prisma.InputJsonValue }),
      },
      include: { machine: { select: { id: true, name: true, type: true, fields: true } } },
    });
  }

  async bulkUpdateLayout(widgets: Array<{ id: string; layout: object }>) {
    const updates = widgets.map(w =>
      prisma.dashboardWidget.update({
        where: { id: w.id },
        data: { layout: w.layout as Prisma.InputJsonValue },
      }),
    );
    return prisma.$transaction(updates);
  }

  async deleteWidget(widgetId: string) {
    return prisma.dashboardWidget.delete({ where: { id: widgetId } });
  }

  async findWidget(widgetId: string) {
    return prisma.dashboardWidget.findUnique({
      where: { id: widgetId },
      include: { dashboard: true },
    });
  }
}
