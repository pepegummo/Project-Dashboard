/**
 * AI Tool Layer — exposes structured tools that an AI agent can call
 * to interact with the platform. Designed for LLM tool-use patterns.
 */

import { MachineService } from '../modules/machines/machine.service';
import { TelemetryService } from '../modules/telemetry/telemetry.service';
import { DashboardService } from '../modules/dashboards/dashboard.service';
import { AlertService } from '../modules/alerts/alert.service';
import { AiContext, AiTool } from '../types';
import { prisma } from '../config/database';

export class AiToolsService {
  private machineService = new MachineService();
  private telemetryService = new TelemetryService();
  private dashboardService = new DashboardService();
  private alertService = new AlertService();

  /** Returns the full tool schema for LLM tool-use */
  getToolDefinitions(): Array<{ name: string; description: string; parameters: object }> {
    return this.getTools().map(({ name, description, parameters }) => ({
      name,
      description,
      parameters,
    }));
  }

  /** Executes a named tool with the given parameters */
  async executeTool(toolName: string, params: Record<string, unknown>, context: AiContext): Promise<unknown> {
    const tool = this.getTools().find(t => t.name === toolName);
    if (!tool) throw new Error(`Unknown tool: ${toolName}`);
    return tool.handler(params, context);
  }

  /** Save AI message */
  async saveMessage(conversationId: string, role: string, content: string, extra?: {
    toolName?: string;
    toolInput?: object;
    toolResult?: object;
  }) {
    return prisma.aiMessage.create({
      data: { conversationId, role, content, ...extra },
    });
  }

  async createConversation(userId: string, title?: string) {
    return prisma.aiConversation.create({
      data: { userId, title: title ?? 'New Conversation' },
    });
  }

  async getConversations(userId: string) {
    return prisma.aiConversation.findMany({
      where: { userId },
      include: { _count: { select: { messages: true } } },
      orderBy: { updatedAt: 'desc' },
      take: 50,
    });
  }

  async getConversationMessages(conversationId: string) {
    return prisma.aiMessage.findMany({
      where: { conversationId },
      orderBy: { createdAt: 'asc' },
    });
  }

  private getTools(): AiTool[] {
    return [
      // ─── getMachines ──────────────────────────────────────────────────────
      {
        name: 'getMachines',
        description: 'Returns a list of all machines with their current status and metadata',
        parameters: {
          type: 'object',
          properties: {
            type: { type: 'string', description: 'Filter by machine type (optional)' },
            status: { type: 'string', description: 'Filter by status: online | offline | maintenance' },
          },
        },
        handler: async (params, ctx) => {
          return this.machineService.getMachines(ctx.organizationId, params as Record<string, string>);
        },
      },

      // ─── getMachineFields ─────────────────────────────────────────────────
      {
        name: 'getMachineFields',
        description: 'Returns the telemetry field schema for a specific machine',
        parameters: {
          type: 'object',
          required: ['machineId'],
          properties: {
            machineId: { type: 'string', description: 'UUID of the machine' },
          },
        },
        handler: async (params, ctx) => {
          return this.machineService.getMachineFields(params.machineId as string, ctx.organizationId);
        },
      },

      // ─── getTelemetry ─────────────────────────────────────────────────────
      {
        name: 'getTelemetry',
        description: 'Retrieves time-series telemetry data for a machine field',
        parameters: {
          type: 'object',
          required: ['machineId', 'field'],
          properties: {
            machineId: { type: 'string' },
            field: { type: 'string', description: 'Field key e.g. weight, temp' },
            timeRange: { type: 'string', description: '5m|15m|30m|1h|6h|24h|7d', default: '1h' },
          },
        },
        handler: async (params, ctx) => {
          return this.telemetryService.getSeries(
            params.machineId as string,
            params.field as string,
            (params.timeRange as string) ?? '1h',
            ctx.organizationId,
          );
        },
      },

      // ─── getLatestTelemetry ───────────────────────────────────────────────
      {
        name: 'getLatestTelemetry',
        description: 'Gets the most recent telemetry snapshot for a machine',
        parameters: {
          type: 'object',
          required: ['machineId'],
          properties: {
            machineId: { type: 'string' },
          },
        },
        handler: async (params, ctx) => {
          return this.telemetryService.getLatest(params.machineId as string, ctx.organizationId);
        },
      },

      // ─── getDashboards ────────────────────────────────────────────────────
      {
        name: 'getDashboards',
        description: 'Lists all dashboards for the current user',
        parameters: { type: 'object', properties: {} },
        handler: async (_params, ctx) => {
          return this.dashboardService.getDashboards(ctx.organizationId, ctx.userId);
        },
      },

      // ─── createWidget ──────────────────────────────────────────────────────
      {
        name: 'createWidget',
        description: 'Adds a new widget to a dashboard',
        parameters: {
          type: 'object',
          required: ['dashboardId', 'widgetType', 'layout', 'config'],
          properties: {
            dashboardId: { type: 'string' },
            machineId: { type: 'string', description: 'Optional machine UUID' },
            widgetType: { type: 'string', enum: ['line-chart', 'gauge', 'kpi-card', 'status-card', 'table', 'alarm-panel'] },
            title: { type: 'string' },
            layout: {
              type: 'object',
              required: ['x', 'y', 'w', 'h'],
              properties: {
                x: { type: 'number' }, y: { type: 'number' },
                w: { type: 'number' }, h: { type: 'number' },
              },
            },
            config: { type: 'object', description: 'Widget-specific config (field, timeRange, etc.)' },
          },
        },
        handler: async (params, ctx) => {
          return this.dashboardService.addWidget(
            params.dashboardId as string,
            ctx.organizationId,
            params as any,
          );
        },
      },

      // ─── updateWidget ──────────────────────────────────────────────────────
      {
        name: 'updateWidget',
        description: 'Updates widget config or title',
        parameters: {
          type: 'object',
          required: ['widgetId'],
          properties: {
            widgetId: { type: 'string' },
            title: { type: 'string' },
            config: { type: 'object' },
          },
        },
        handler: async (params, ctx) => {
          return this.dashboardService.updateWidget(params.widgetId as string, ctx.organizationId, params as any);
        },
      },

      // ─── moveWidget ───────────────────────────────────────────────────────
      {
        name: 'moveWidget',
        description: 'Moves and/or resizes a widget on the grid',
        parameters: {
          type: 'object',
          required: ['widgetId', 'layout'],
          properties: {
            widgetId: { type: 'string' },
            layout: {
              type: 'object',
              properties: {
                x: { type: 'number' }, y: { type: 'number' },
                w: { type: 'number' }, h: { type: 'number' },
              },
            },
          },
        },
        handler: async (params, ctx) => {
          return this.dashboardService.updateWidget(
            params.widgetId as string,
            ctx.organizationId,
            { layout: params.layout as object },
          );
        },
      },

      // ─── createAlert ──────────────────────────────────────────────────────
      {
        name: 'createAlert',
        description: 'Creates an alert rule for a machine field threshold',
        parameters: {
          type: 'object',
          required: ['machineId', 'name', 'field', 'condition', 'threshold', 'severity'],
          properties: {
            machineId: { type: 'string' },
            name: { type: 'string' },
            field: { type: 'string' },
            condition: { type: 'string', enum: ['gt', 'lt', 'gte', 'lte', 'eq', 'between', 'outside'] },
            threshold: { type: 'number' },
            thresholdHi: { type: 'number' },
            severity: { type: 'string', enum: ['info', 'warning', 'critical'] },
          },
        },
        handler: async (params, ctx) => {
          return this.alertService.createAlert(ctx.organizationId, params as any);
        },
      },

      // ─── getActiveAlerts ──────────────────────────────────────────────────
      {
        name: 'getActiveAlerts',
        description: 'Returns currently active (open) alert events',
        parameters: { type: 'object', properties: {} },
        handler: async (_params, ctx) => {
          return this.alertService.getActiveEvents(ctx.organizationId);
        },
      },
    ];
  }
}
