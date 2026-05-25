import WebSocket from 'ws';

export interface ExtendedWebSocket extends WebSocket {
  id: string;
  userId?: string;
  organizationId?: string;
  subscribedMachines: Set<string>;
  isAlive: boolean;
}
