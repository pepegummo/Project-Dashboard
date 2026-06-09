package websocket

import (
	"encoding/json"
	"sync"
	"time"

	fiberws "github.com/gofiber/websocket/v2"
)

// WebSocket message type constants (RFC 6455 §11.8)
const (
	wsText  = 1
	wsClose = 8
	wsPing  = 9
)

const (
	TypeTelemetry     = "telemetry"
	TypeAlert         = "alert"
	TypeMachineStatus = "machine_status"
	TypeSubscribe     = "subscribe"
	TypeUnsubscribe   = "unsubscribe"
	TypePing          = "ping"
	TypePong          = "pong"
	TypeError         = "error"
)

type Message struct {
	Type      string      `json:"type"`
	Payload   interface{} `json:"payload"`
	Timestamp int64       `json:"timestamp"`
}

type TelemetryPayload struct {
	MachineID   string                 `json:"machineId"`
	MachineName string                 `json:"machineName"`
	Timestamp   string                 `json:"timestamp"`
	Data        map[string]interface{} `json:"data"`
}

type AlertPayload struct {
	AlertID     string  `json:"alertId"`
	AlertName   string  `json:"alertName"`
	MachineID   string  `json:"machineId"`
	MachineName string  `json:"machineName"`
	Field       string  `json:"field"`
	Value       float64 `json:"value"`
	Threshold   float64 `json:"threshold"`
	Condition   string  `json:"condition"`
	Severity    string  `json:"severity"`
	Message     string  `json:"message"`
	Timestamp   string  `json:"timestamp"`
}

type MachineStatusPayload struct {
	MachineID   string `json:"machineId"`
	MachineName string `json:"machineName"`
	Status      string `json:"status"`
	Timestamp   string `json:"timestamp"`
}

type client struct {
	conn          *fiberws.Conn
	send          chan []byte
	subscriptions map[string]struct{}
	mu            sync.Mutex
}

type Gateway struct {
	clients map[*client]struct{}
	mu      sync.RWMutex
}

func NewGateway() *Gateway {
	return &Gateway{
		clients: make(map[*client]struct{}),
	}
}

// HandleFiber is the Fiber WebSocket handler — registered via fiberws.New(gateway.HandleFiber).
func (g *Gateway) HandleFiber(c *fiberws.Conn) {
	cl := &client{
		conn:          c,
		send:          make(chan []byte, 256),
		subscriptions: make(map[string]struct{}),
	}
	g.mu.Lock()
	g.clients[cl] = struct{}{}
	g.mu.Unlock()

	go cl.writePump()
	cl.readPump(g)
}

func (c *client) readPump(g *Gateway) {
	defer func() {
		g.mu.Lock()
		delete(g.clients, c)
		g.mu.Unlock()
		c.conn.Close()
	}()

	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			break
		}

		var m Message
		if err := json.Unmarshal(msg, &m); err != nil {
			continue
		}

		switch m.Type {
		case TypePing:
			c.sendJSON(Message{Type: TypePong, Payload: map[string]interface{}{}, Timestamp: nowMs()})

		case TypeSubscribe:
			if payload, ok := m.Payload.(map[string]interface{}); ok {
				if ids, ok := payload["machineIds"].([]interface{}); ok {
					c.mu.Lock()
					for _, id := range ids {
						if s, ok := id.(string); ok {
							c.subscriptions[s] = struct{}{}
						}
					}
					c.mu.Unlock()
				}
			}

		case TypeUnsubscribe:
			if payload, ok := m.Payload.(map[string]interface{}); ok {
				if ids, ok := payload["machineIds"].([]interface{}); ok {
					c.mu.Lock()
					for _, id := range ids {
						if s, ok := id.(string); ok {
							delete(c.subscriptions, s)
						}
					}
					c.mu.Unlock()
				}
			}
		}
	}
}

func (c *client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				_ = c.conn.WriteMessage(wsClose, []byte{})
				return
			}
			if err := c.conn.WriteMessage(wsText, msg); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.conn.WriteMessage(wsPing, nil); err != nil {
				return
			}
		}
	}
}

func (c *client) sendJSON(v interface{}) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	select {
	case c.send <- b:
	default:
	}
}

func (c *client) isSubscribed(machineID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.subscriptions) == 0 {
		return true
	}
	_, ok := c.subscriptions[machineID]
	return ok
}

func (g *Gateway) BroadcastTelemetry(payload TelemetryPayload) {
	msg := Message{Type: TypeTelemetry, Payload: payload, Timestamp: nowMs()}
	g.mu.RLock()
	defer g.mu.RUnlock()
	for c := range g.clients {
		if c.isSubscribed(payload.MachineID) {
			c.sendJSON(msg)
		}
	}
}

func (g *Gateway) BroadcastAlert(payload AlertPayload) {
	msg := Message{Type: TypeAlert, Payload: payload, Timestamp: nowMs()}
	g.mu.RLock()
	defer g.mu.RUnlock()
	for c := range g.clients {
		c.sendJSON(msg)
	}
}

func (g *Gateway) BroadcastMachineStatus(payload MachineStatusPayload) {
	msg := Message{Type: TypeMachineStatus, Payload: payload, Timestamp: nowMs()}
	g.mu.RLock()
	defer g.mu.RUnlock()
	for c := range g.clients {
		c.sendJSON(msg)
	}
}

func nowMs() int64 { return time.Now().UnixMilli() }
