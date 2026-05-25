import { Request } from 'express';

// ─── Auth ─────────────────────────────────────────────────────────────────────
export interface JwtPayload {
  sub: string;       // userId
  orgId: string;     // organizationId
  role: string;
  email: string;
  iat?: number;
  exp?: number;
}

export interface AuthenticatedRequest extends Request {
  user: JwtPayload;
}

// ─── API Responses ────────────────────────────────────────────────────────────
export interface ApiResponse<T = unknown> {
  success: true;
  data: T;
  meta?: PaginationMeta;
}

export interface ApiError {
  success: false;
  error: {
    code: string;
    message: string;
    details?: unknown;
  };
}

export interface PaginationMeta {
  total: number;
  page: number;
  limit: number;
  totalPages: number;
}

export interface PaginationQuery {
  page?: number;
  limit?: number;
  sortBy?: string;
  sortOrder?: 'asc' | 'desc';
}

// ─── Telemetry ───────────────────────────────────────────────────────────────
export interface TelemetryData {
  [key: string]: number | boolean | string;
}

export interface TelemetryPoint {
  machineId: string;
  timestamp: Date;
  data: TelemetryData;
  quality?: 'good' | 'bad' | 'uncertain';
}

// ─── WebSocket ───────────────────────────────────────────────────────────────
export type WsMessageType =
  | 'telemetry'
  | 'alert'
  | 'machine_status'
  | 'subscribe'
  | 'unsubscribe'
  | 'ping'
  | 'pong'
  | 'error';

export interface WsMessage<T = unknown> {
  type: WsMessageType;
  payload: T;
  timestamp: number;
}

export interface WsSubscribePayload {
  machineIds: string[];
}

export interface WsTelemetryPayload {
  machineId: string;
  machineName: string;
  timestamp: string;
  data: TelemetryData;
}

export interface WsAlertPayload {
  alertId: string;
  alertName: string;
  machineId: string;
  machineName: string;
  field: string;
  value: number;
  threshold: number;
  condition: string;
  severity: 'info' | 'warning' | 'critical';
  message: string;
  timestamp: string;
}

// ─── AI Tools ────────────────────────────────────────────────────────────────
export interface AiTool {
  name: string;
  description: string;
  parameters: Record<string, unknown>;
  handler: (params: Record<string, unknown>, context: AiContext) => Promise<unknown>;
}

export interface AiContext {
  userId: string;
  organizationId: string;
  activeDashboardId?: string;
}
