import { DashboardRepository } from './dashboard.repository';
import { AppError } from '../../middleware/error';

export class DashboardService {
  private repo = new DashboardRepository();

  async getDashboards(organizationId: string, userId: string) {
    return this.repo.findAll(organizationId, userId);
  }

  async getDashboardById(id: string, organizationId: string) {
    const dashboard = await this.repo.findById(id);
    if (!dashboard || dashboard.organizationId !== organizationId) {
      throw new AppError(404, 'NOT_FOUND', 'Dashboard not found');
    }
    return dashboard;
  }

  async createDashboard(data: {
    organizationId: string;
    userId: string;
    name: string;
    description?: string;
    isPublic?: boolean;
    tags?: string[];
  }) {
    return this.repo.create({
      organization: { connect: { id: data.organizationId } },
      user: { connect: { id: data.userId } },
      name: data.name,
      description: data.description,
      isPublic: data.isPublic ?? false,
      tags: data.tags ?? [],
    });
  }

  async updateDashboard(id: string, organizationId: string, data: {
    name?: string;
    description?: string;
    isPublic?: boolean;
    tags?: string[];
  }) {
    await this.getDashboardById(id, organizationId);
    return this.repo.update(id, data);
  }

  async deleteDashboard(id: string, organizationId: string, userId: string, userRole: string) {
    const dashboard = await this.getDashboardById(id, organizationId);
    if (dashboard.userId !== userId && userRole !== 'admin') {
      throw new AppError(403, 'FORBIDDEN', 'You can only delete your own dashboards');
    }
    return this.repo.delete(id);
  }

  // ─── Widgets ──────────────────────────────────────────────────────────────
  async addWidget(dashboardId: string, organizationId: string, widget: {
    machineId?: string;
    widgetType: string;
    title?: string;
    layout: object;
    config: object;
  }) {
    await this.getDashboardById(dashboardId, organizationId);
    return this.repo.addWidget(dashboardId, widget);
  }

  async updateWidget(widgetId: string, organizationId: string, data: {
    machineId?: string | null;
    title?: string;
    layout?: object;
    config?: object;
  }) {
    const widget = await this.repo.findWidget(widgetId);
    if (!widget || widget.dashboard.organizationId !== organizationId) {
      throw new AppError(404, 'NOT_FOUND', 'Widget not found');
    }
    return this.repo.updateWidget(widgetId, data);
  }

  async bulkUpdateLayout(dashboardId: string, organizationId: string, widgets: Array<{ id: string; layout: object }>) {
    await this.getDashboardById(dashboardId, organizationId);
    return this.repo.bulkUpdateLayout(widgets);
  }

  async deleteWidget(widgetId: string, organizationId: string) {
    const widget = await this.repo.findWidget(widgetId);
    if (!widget || widget.dashboard.organizationId !== organizationId) {
      throw new AppError(404, 'NOT_FOUND', 'Widget not found');
    }
    return this.repo.deleteWidget(widgetId);
  }
}
