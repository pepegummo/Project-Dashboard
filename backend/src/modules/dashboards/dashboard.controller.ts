import { Request, Response, NextFunction } from 'express';
import { z } from 'zod';
import { DashboardService } from './dashboard.service';
import { AuthenticatedRequest } from '../../types';

const layoutSchema = z.object({
  x: z.number().int().min(0),
  y: z.number().int().min(0),
  w: z.number().int().min(1).max(12),
  h: z.number().int().min(1).max(24),
});

const addWidgetSchema = z.object({
  machineId: z.string().uuid().optional(),
  widgetType: z.enum(['line-chart', 'gauge', 'kpi-card', 'status-card', 'table', 'alarm-panel', 'daily-count']),
  title: z.string().max(100).optional(),
  layout: layoutSchema,
  config: z.record(z.unknown()),
});

const bulkLayoutSchema = z.object({
  widgets: z.array(z.object({
    id: z.string().uuid(),
    layout: layoutSchema,
  })),
});

export class DashboardController {
  private svc = new DashboardService();

  list = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId, sub } = (req as AuthenticatedRequest).user;
      const dashboards = await this.svc.getDashboards(orgId, sub);
      res.json({ success: true, data: dashboards });
    } catch (err) { next(err); }
  };

  getById = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const dashboard = await this.svc.getDashboardById(req.params.id, orgId);
      res.json({ success: true, data: dashboard });
    } catch (err) { next(err); }
  };

  create = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId, sub } = (req as AuthenticatedRequest).user;
      const body = z.object({
        name: z.string().min(1).max(100),
        description: z.string().optional(),
        isPublic: z.boolean().optional(),
        tags: z.array(z.string()).optional(),
      }).parse(req.body);
      const dashboard = await this.svc.createDashboard({ organizationId: orgId, userId: sub, ...body });
      res.status(201).json({ success: true, data: dashboard });
    } catch (err) { next(err); }
  };

  update = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const dashboard = await this.svc.updateDashboard(req.params.id, orgId, req.body);
      res.json({ success: true, data: dashboard });
    } catch (err) { next(err); }
  };

  delete = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId, sub, role } = (req as AuthenticatedRequest).user;
      await this.svc.deleteDashboard(req.params.id, orgId, sub, role);
      res.json({ success: true, data: { deleted: true } });
    } catch (err) { next(err); }
  };

  addWidget = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const body = addWidgetSchema.parse(req.body);
      const widget = await this.svc.addWidget(req.params.id, orgId, body);
      res.status(201).json({ success: true, data: widget });
    } catch (err) { next(err); }
  };

  updateWidget = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const widget = await this.svc.updateWidget(req.params.widgetId, orgId, req.body);
      res.json({ success: true, data: widget });
    } catch (err) { next(err); }
  };

  bulkUpdateLayout = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const { widgets } = bulkLayoutSchema.parse(req.body);
      await this.svc.bulkUpdateLayout(req.params.id, orgId, widgets);
      res.json({ success: true, data: { updated: widgets.length } });
    } catch (err) { next(err); }
  };

  deleteWidget = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      await this.svc.deleteWidget(req.params.widgetId, orgId);
      res.json({ success: true, data: { deleted: true } });
    } catch (err) { next(err); }
  };
}
