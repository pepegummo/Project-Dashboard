import { WebSocketServer, WebSocket } from 'ws';
import http from 'http';
import { v4 as uuidv4 } from 'uuid';
import jwt from 'jsonwebtoken';
import { env } from '../config/env';
import { ExtendedWebSocket } from './ws.types';
import { WsMessage, WsTelemetryPayload, WsAlertPayload, JwtPayload } from '../types';

export class WsGateway {
  private wss: WebSocketServer;
  private clients = new Map<string, ExtendedWebSocket>();
  private pingInterval: NodeJS.Timeout | null = null;

  constructor(server?: http.Server) {
    this.wss = server
      ? new WebSocketServer({ server })
      : new WebSocketServer({ port: env.WS_PORT });

    this.wss.on('connection', this.handleConnection.bind(this));
    this.startPingLoop();

    console.log(`🔌 WebSocket server ready on port ${env.WS_PORT}`);
  }

  private handleConnection(ws: WebSocket, req: http.IncomingMessage): void {
    const extWs = ws as ExtendedWebSocket;
    extWs.id = uuidv4();
    extWs.subscribedMachines = new Set();
    extWs.isAlive = true;

    // Try to authenticate from query string token
    const url = new URL(req.url ?? '/', `http://localhost`);
    const token = url.searchParams.get('token');
    if (token) {
      try {
        const payload = jwt.verify(token, env.JWT_SECRET) as JwtPayload;
        extWs.userId = payload.sub;
        extWs.organizationId = payload.orgId;
      } catch {
        // Unauthenticated connection — still allow read-only broadcasts
      }
    }

    this.clients.set(extWs.id, extWs);
    console.log(`[WS] Client connected: ${extWs.id} (clients: ${this.clients.size})`);

    extWs.on('message', (raw) => this.handleMessage(extWs, raw.toString()));
    extWs.on('pong', () => { extWs.isAlive = true; });
    extWs.on('close', () => {
      this.clients.delete(extWs.id);
      console.log(`[WS] Client disconnected: ${extWs.id} (clients: ${this.clients.size})`);
    });
    extWs.on('error', (err) => console.error(`[WS] Client error ${extWs.id}:`, err));

    this.send(extWs, { type: 'pong', payload: { connectionId: extWs.id }, timestamp: Date.now() });
  }

  private handleMessage(ws: ExtendedWebSocket, raw: string): void {
    try {
      const msg: WsMessage = JSON.parse(raw);
      switch (msg.type) {
        case 'subscribe': {
          const { machineIds } = msg.payload as { machineIds: string[] };
          if (Array.isArray(machineIds)) {
            machineIds.forEach(id => ws.subscribedMachines.add(id));
          }
          this.send(ws, {
            type: 'pong',
            payload: { subscribed: Array.from(ws.subscribedMachines) },
            timestamp: Date.now(),
          });
          break;
        }
        case 'unsubscribe': {
          const { machineIds } = msg.payload as { machineIds: string[] };
          if (Array.isArray(machineIds)) {
            machineIds.forEach(id => ws.subscribedMachines.delete(id));
          }
          break;
        }
        case 'ping': {
          this.send(ws, { type: 'pong', payload: {}, timestamp: Date.now() });
          break;
        }
      }
    } catch {
      // Ignore malformed messages
    }
  }

  /** Broadcast telemetry to all subscribed clients */
  broadcastTelemetry(payload: WsTelemetryPayload): void {
    const msg: WsMessage<WsTelemetryPayload> = {
      type: 'telemetry',
      payload,
      timestamp: Date.now(),
    };
    const json = JSON.stringify(msg);
    for (const client of this.clients.values()) {
      if (client.readyState === WebSocket.OPEN &&
          (client.subscribedMachines.size === 0 || client.subscribedMachines.has(payload.machineId))) {
        client.send(json);
      }
    }
  }

  /** Broadcast alert event to all connected clients */
  broadcastAlert(payload: WsAlertPayload): void {
    const msg: WsMessage<WsAlertPayload> = {
      type: 'alert',
      payload,
      timestamp: Date.now(),
    };
    const json = JSON.stringify(msg);
    for (const client of this.clients.values()) {
      if (client.readyState === WebSocket.OPEN) {
        client.send(json);
      }
    }
  }

  /** Broadcast machine status change */
  broadcastMachineStatus(machineId: string, status: string, machineName: string): void {
    const msg: WsMessage = {
      type: 'machine_status',
      payload: { machineId, machineName, status, timestamp: new Date().toISOString() },
      timestamp: Date.now(),
    };
    const json = JSON.stringify(msg);
    for (const client of this.clients.values()) {
      if (client.readyState === WebSocket.OPEN) {
        client.send(json);
      }
    }
  }

  private send(ws: ExtendedWebSocket, msg: WsMessage): void {
    if (ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(msg));
    }
  }

  private startPingLoop(): void {
    this.pingInterval = setInterval(() => {
      for (const client of this.clients.values()) {
        if (!client.isAlive) {
          client.terminate();
          this.clients.delete(client.id);
          continue;
        }
        client.isAlive = false;
        client.ping();
      }
    }, 30_000);
  }

  get clientCount(): number {
    return this.clients.size;
  }

  close(): void {
    if (this.pingInterval) clearInterval(this.pingInterval);
    this.wss.close();
  }
}
