import type { WsMessage, WsTelemetryPayload, WsAlertPayload } from '@/types';

type MessageHandler<T = unknown> = (payload: T) => void;

const WS_URL = import.meta.env.VITE_WS_URL ?? 'ws://localhost:4001';

class WebSocketService {
  private ws: WebSocket | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private reconnectDelay = 2000;
  private maxReconnectDelay = 30000;
  private shouldConnect = false;

  private telemetryHandlers = new Map<string, Set<MessageHandler<WsTelemetryPayload>>>();
  private alertHandlers = new Set<MessageHandler<WsAlertPayload>>();
  private statusHandlers = new Set<MessageHandler<{ machineId: string; status: string; machineName: string }>>();
  private connectHandlers = new Set<() => void>();
  private disconnectHandlers = new Set<() => void>();

  connect(token?: string | null): void {
    this.shouldConnect = true;
    const url = token ? `${WS_URL}?token=${encodeURIComponent(token)}` : WS_URL;
    this.openConnection(url);
  }

  private openConnection(url: string): void {
    if (this.ws?.readyState === WebSocket.OPEN) return;

    try {
      this.ws = new WebSocket(url);

      this.ws.onopen = () => {
        console.log('[WS] Connected');
        this.reconnectDelay = 2000;
        this.connectHandlers.forEach(h => h());
      };

      this.ws.onmessage = (event: MessageEvent) => {
        try {
          const msg: WsMessage = JSON.parse(event.data);
          this.dispatch(msg);
        } catch { /* ignore */ }
      };

      this.ws.onclose = () => {
        console.log('[WS] Disconnected');
        this.disconnectHandlers.forEach(h => h());
        if (this.shouldConnect) this.scheduleReconnect(url);
      };

      this.ws.onerror = (err) => {
        console.warn('[WS] Error:', err);
      };
    } catch (err) {
      console.warn('[WS] Failed to connect:', err);
      if (this.shouldConnect) this.scheduleReconnect(url);
    }
  }

  private scheduleReconnect(url: string): void {
    if (this.reconnectTimer) return;
    console.log(`[WS] Reconnecting in ${this.reconnectDelay}ms…`);
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.reconnectDelay = Math.min(this.reconnectDelay * 1.5, this.maxReconnectDelay);
      this.openConnection(url);
    }, this.reconnectDelay);
  }

  disconnect(): void {
    this.shouldConnect = false;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.ws?.close();
    this.ws = null;
  }

  subscribe(machineIds: string[]): void {
    this.send({ type: 'subscribe', payload: { machineIds }, timestamp: Date.now() });
  }

  unsubscribe(machineIds: string[]): void {
    this.send({ type: 'unsubscribe', payload: { machineIds }, timestamp: Date.now() });
  }

  private send(msg: WsMessage): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg));
    }
  }

  private dispatch(msg: WsMessage): void {
    switch (msg.type) {
      case 'telemetry': {
        const payload = msg.payload as WsTelemetryPayload;
        // Notify machine-specific handlers
        const handlers = this.telemetryHandlers.get(payload.machineId);
        handlers?.forEach(h => h(payload));
        // Notify wildcard handlers (key = '*')
        this.telemetryHandlers.get('*')?.forEach(h => h(payload));
        break;
      }
      case 'alert':
        this.alertHandlers.forEach(h => h(msg.payload as WsAlertPayload));
        break;
      case 'machine_status':
        this.statusHandlers.forEach(h => h(msg.payload as any));
        break;
    }
  }

  // ─── Event API ─────────────────────────────────────────────────────────────
  onTelemetry(machineId: string, handler: MessageHandler<WsTelemetryPayload>): () => void {
    if (!this.telemetryHandlers.has(machineId)) {
      this.telemetryHandlers.set(machineId, new Set());
    }
    this.telemetryHandlers.get(machineId)!.add(handler);
    return () => this.telemetryHandlers.get(machineId)?.delete(handler);
  }

  onAlert(handler: MessageHandler<WsAlertPayload>): () => void {
    this.alertHandlers.add(handler);
    return () => this.alertHandlers.delete(handler);
  }

  onMachineStatus(handler: MessageHandler<any>): () => void {
    this.statusHandlers.add(handler);
    return () => this.statusHandlers.delete(handler);
  }

  onConnect(handler: () => void): () => void {
    this.connectHandlers.add(handler);
    return () => this.connectHandlers.delete(handler);
  }

  onDisconnect(handler: () => void): () => void {
    this.disconnectHandlers.add(handler);
    return () => this.disconnectHandlers.delete(handler);
  }

  get isConnected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN;
  }
}

export const wsService = new WebSocketService();
