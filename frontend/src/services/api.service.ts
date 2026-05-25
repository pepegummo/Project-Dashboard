import axios, { AxiosInstance, AxiosError } from 'axios';
import type { ApiResponse, Dashboard, DashboardWidget, Machine, MachineField, Alert, AlertEvent, AiConversation, AiMessage, AiTool, TelemetrySeries, TelemetrySnapshot } from '@/types';

const BASE_URL = import.meta.env.VITE_API_URL ?? 'http://localhost:4000';

class ApiService {
  private client: AxiosInstance;

  constructor() {
    this.client = axios.create({
      baseURL: `${BASE_URL}/api`,
      timeout: 15_000,
      headers: { 'Content-Type': 'application/json' },
    });

    // Request interceptor — attach token
    this.client.interceptors.request.use((config) => {
      const token = localStorage.getItem('auth_token');
      if (token) config.headers.Authorization = `Bearer ${token}`;
      return config;
    });

    // Response interceptor — unwrap or throw
    this.client.interceptors.response.use(
      (res) => res,
      (err: AxiosError<{ success: false; error: { code: string; message: string } }>) => {
        if (err.response?.status === 401) {
          localStorage.removeItem('auth_token');
          window.location.href = '/login';
        }
        const message = err.response?.data?.error?.message ?? err.message;
        return Promise.reject(new Error(message));
      },
    );
  }

  setToken(token: string | null) {
    if (token) {
      localStorage.setItem('auth_token', token);
    } else {
      localStorage.removeItem('auth_token');
    }
  }

  // ─── Auth ──────────────────────────────────────────────────────────────────
  async login(email: string, password: string) {
    const { data } = await this.client.post<ApiResponse<{ token: string; user: any }>>('/auth/login', { email, password });
    return data.data;
  }

  async getProfile() {
    const { data } = await this.client.get<ApiResponse<any>>('/auth/me');
    return data.data;
  }

  // ─── Machines ─────────────────────────────────────────────────────────────
  async getMachines(filters?: Record<string, string>) {
    const { data } = await this.client.get<ApiResponse<Machine[]>>('/machines', { params: filters });
    return data.data;
  }

  async getMachine(id: string) {
    const { data } = await this.client.get<ApiResponse<Machine>>(`/machines/${id}`);
    return data.data;
  }

  async createMachine(payload: Partial<Machine> & { productionLineId: string }) {
    const { data } = await this.client.post<ApiResponse<Machine>>('/machines', payload);
    return data.data;
  }

  async updateMachine(id: string, payload: Partial<Machine>) {
    const { data } = await this.client.patch<ApiResponse<Machine>>(`/machines/${id}`, payload);
    return data.data;
  }

  async deleteMachine(id: string) {
    await this.client.delete(`/machines/${id}`);
  }

  async getMachineFields(machineId: string) {
    const { data } = await this.client.get<ApiResponse<MachineField[]>>(`/machines/${machineId}/fields`);
    return data.data;
  }

  async upsertMachineField(machineId: string, field: Partial<MachineField>) {
    const { data } = await this.client.put<ApiResponse<MachineField>>(`/machines/${machineId}/fields`, field);
    return data.data;
  }

  async getProductionLines() {
    const { data } = await this.client.get<ApiResponse<any[]>>('/machines/production-lines');
    return data.data;
  }

  async getFactories() {
    const { data } = await this.client.get<ApiResponse<any[]>>('/machines/factories');
    return data.data;
  }

  // ─── Telemetry ────────────────────────────────────────────────────────────
  async getLatestTelemetry(machineId: string) {
    const { data } = await this.client.get<ApiResponse<TelemetrySnapshot>>(`/telemetry/${machineId}/latest`);
    return data.data;
  }

  async getTelemetrySeries(machineId: string, field: string, timeRange = '1h') {
    const { data } = await this.client.get<ApiResponse<TelemetrySeries>>(`/telemetry/${machineId}/series`, {
      params: { field, timeRange },
    });
    return data.data;
  }

  async getTelemetryAggregate(machineId: string, field: string, period: string) {
    const { data } = await this.client.get<ApiResponse<import('@/types').TelemetryAggregateResult>>(
      `/telemetry/${machineId}/aggregate`,
      { params: { field, period } },
    );
    return data.data;
  }

  async getMultiLatestTelemetry(machineIds: string[]) {
    const { data } = await this.client.get<ApiResponse<Record<string, TelemetrySnapshot>>>('/telemetry/latest', {
      params: { ids: machineIds.join(',') },
    });
    return data.data;
  }

  // ─── Dashboards ───────────────────────────────────────────────────────────
  async getDashboards() {
    const { data } = await this.client.get<ApiResponse<Dashboard[]>>('/dashboards');
    return data.data;
  }

  async getDashboard(id: string) {
    const { data } = await this.client.get<ApiResponse<Dashboard>>(`/dashboards/${id}`);
    return data.data;
  }

  async createDashboard(payload: { name: string; description?: string; isPublic?: boolean; tags?: string[] }) {
    const { data } = await this.client.post<ApiResponse<Dashboard>>('/dashboards', payload);
    return data.data;
  }

  async updateDashboard(id: string, payload: Partial<Dashboard>) {
    const { data } = await this.client.patch<ApiResponse<Dashboard>>(`/dashboards/${id}`, payload);
    return data.data;
  }

  async deleteDashboard(id: string) {
    await this.client.delete(`/dashboards/${id}`);
  }

  async addWidget(dashboardId: string, widget: {
    machineId?: string;
    widgetType: string;
    title?: string;
    layout: { x: number; y: number; w: number; h: number };
    config: Record<string, unknown>;
  }) {
    const { data } = await this.client.post<ApiResponse<DashboardWidget>>(`/dashboards/${dashboardId}/widgets`, widget);
    return data.data;
  }

  async updateWidget(dashboardId: string, widgetId: string, payload: Partial<DashboardWidget>) {
    const { data } = await this.client.patch<ApiResponse<DashboardWidget>>(`/dashboards/${dashboardId}/widgets/${widgetId}`, payload);
    return data.data;
  }

  async bulkUpdateLayout(dashboardId: string, widgets: Array<{ id: string; layout: any }>) {
    await this.client.patch(`/dashboards/${dashboardId}/layout`, { widgets });
  }

  async deleteWidget(dashboardId: string, widgetId: string) {
    await this.client.delete(`/dashboards/${dashboardId}/widgets/${widgetId}`);
  }

  // ─── Alerts ───────────────────────────────────────────────────────────────
  async getAlerts(machineId?: string) {
    const { data } = await this.client.get<ApiResponse<Alert[]>>('/alerts', { params: machineId ? { machineId } : {} });
    return data.data;
  }

  async getActiveAlertEvents() {
    const { data } = await this.client.get<ApiResponse<AlertEvent[]>>('/alerts/events/active');
    return data.data;
  }

  async createAlert(payload: Partial<Alert> & { machineId: string }) {
    const { data } = await this.client.post<ApiResponse<Alert>>('/alerts', payload);
    return data.data;
  }

  async updateAlert(id: string, payload: Partial<Alert>) {
    const { data } = await this.client.patch<ApiResponse<Alert>>(`/alerts/${id}`, payload);
    return data.data;
  }

  async deleteAlert(id: string) {
    await this.client.delete(`/alerts/${id}`);
  }

  async acknowledgeAlertEvent(eventId: string) {
    const { data } = await this.client.patch<ApiResponse<AlertEvent>>(`/alerts/events/${eventId}/acknowledge`);
    return data.data;
  }

  async resolveAlertEvent(eventId: string) {
    const { data } = await this.client.patch<ApiResponse<AlertEvent>>(`/alerts/events/${eventId}/resolve`);
    return data.data;
  }

  // ─── AI ───────────────────────────────────────────────────────────────────
  async getAiTools() {
    const { data } = await this.client.get<ApiResponse<AiTool[]>>('/ai/tools');
    return data.data;
  }

  async executeAiTool(toolName: string, params: Record<string, unknown> = {}) {
    const { data } = await this.client.post<ApiResponse<unknown>>('/ai/tools/execute', { toolName, params });
    return data.data;
  }

  async getConversations() {
    const { data } = await this.client.get<ApiResponse<AiConversation[]>>('/ai/conversations');
    return data.data;
  }

  async createConversation(title?: string) {
    const { data } = await this.client.post<ApiResponse<AiConversation>>('/ai/conversations', { title });
    return data.data;
  }

  async getMessages(conversationId: string) {
    const { data } = await this.client.get<ApiResponse<AiMessage[]>>(`/ai/conversations/${conversationId}/messages`);
    return data.data;
  }

  async addMessage(conversationId: string, payload: { role: string; content: string; toolName?: string; toolInput?: object; toolResult?: object }) {
    const { data } = await this.client.post<ApiResponse<AiMessage>>(`/ai/conversations/${conversationId}/messages`, payload);
    return data.data;
  }
}

export const api = new ApiService();
