package simulator

import (
	"context"
	"fmt"
	"iot-dashboard/internal/modules/alerts"
	ws "iot-dashboard/internal/websocket"
	"math"
	"math/rand"
	"time"
)

// Simulator replicates the TypeScript TelemetrySimulator:
// - Pulse wave with random duty cycle each cycle (120 ticks = 2 hours)
// - Noise layers: Gaussian, spikes, sinusoidal drift
// - Broadcasts via WebSocket, evaluates alert rules.

const (
	cycleTicks = 120 // 2 hours at 1-min ticks
	transTicks = 5   // smooth transition between high/low
)

type MachineConfig struct {
	ID   string
	Name string
	Type string
}

type fieldState struct {
	key       string
	threshold float64
	tick      int
	duty      float64 // 0–1
	driftPhase float64
	pwmPhase  float64
}

type Simulator struct {
	gateway      *ws.Gateway
	alertSvc     *alerts.Service
	machines     []MachineConfig
	tickInterval time.Duration
	stop         chan struct{}
}

func NewSimulator(g *ws.Gateway, tickIntervalMs int) *Simulator {
	return &Simulator{
		gateway:      g,
		alertSvc:     alerts.NewService(),
		tickInterval: time.Duration(tickIntervalMs) * time.Millisecond,
		stop:         make(chan struct{}),
	}
}

func (s *Simulator) ConfigureMachines(machines []MachineConfig) {
	s.machines = machines
}

func (s *Simulator) Start() {
	fmt.Printf("🤖 Simulator started — %d machines, tick every %v\n", len(s.machines), s.tickInterval)
	go s.run()
}

func (s *Simulator) Stop() {
	close(s.stop)
}

func (s *Simulator) run() {
	ticker := time.NewTicker(s.tickInterval)
	defer ticker.Stop()

	// Init field states per machine
	states := make(map[string][]fieldState)
	for _, m := range s.machines {
		states[m.ID] = initFields(m.Type)
	}

	tick := 0
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			tick++
			ctx := context.Background()

			for _, m := range s.machines {
				fields := states[m.ID]
				data := make(map[string]interface{})

				for i := range fields {
					f := &fields[i]
					val := f.nextValue(tick)
					data[f.key] = val
				}
				states[m.ID] = fields

				// Broadcast via WebSocket
				s.gateway.BroadcastTelemetry(ws.TelemetryPayload{
					MachineID:   m.ID,
					MachineName: m.Name,
					Timestamp:   time.Now().UTC().Format(time.RFC3339),
					Data:        data,
				})

				// Evaluate alert rules
				triggered, err := s.alertSvc.EvaluateTelemetry(ctx, m.ID, data)
				if err == nil {
					for _, t := range triggered {
						s.gateway.BroadcastAlert(ws.AlertPayload{
							AlertID:   t.AlertID,
							AlertName: t.AlertName,
							MachineID: m.ID,
							MachineName: m.Name,
							Field:     t.Field,
							Value:     t.Value,
							Threshold: t.Threshold,
							Condition: t.Condition,
							Severity:  t.Severity,
							Message:   t.Message,
							Timestamp: time.Now().UTC().Format(time.RFC3339),
						})
					}
				}
			}
		}
	}
}

// initFields returns field states for each machine type.
func initFields(machineType string) []fieldState {
	switch machineType {
	case "checkweigher":
		return []fieldState{
			{key: "weight", threshold: 500, duty: randDuty()},
			{key: "speed", threshold: 60, duty: randDuty()},
			{key: "throughput", threshold: 1200, duty: randDuty()},
			{key: "rejects", threshold: 5, duty: randDuty()},
		}
	case "temperature_sensor":
		return []fieldState{
			{key: "temp", threshold: 22, duty: randDuty()},
			{key: "humidity", threshold: 55, duty: randDuty()},
			{key: "dew_point", threshold: 12, duty: randDuty()},
		}
	case "conveyor":
		return []fieldState{
			{key: "speed", threshold: 1.5, duty: randDuty()},
			{key: "load", threshold: 80, duty: randDuty()},
			{key: "rpm", threshold: 750, duty: randDuty()},
			{key: "vibration", threshold: 0.5, duty: randDuty()},
		}
	case "vision_camera":
		return []fieldState{
			{key: "defect_rate", threshold: 2.0, duty: randDuty()},
			{key: "confidence", threshold: 98.5, duty: randDuty()},
		}
	default:
		return []fieldState{{key: "value", threshold: 100, duty: randDuty()}}
	}
}

func (f *fieldState) nextValue(tick int) float64 {
	// Re-randomise duty cycle at start of each cycle
	cyclePos := tick % cycleTicks
	if cyclePos == 0 {
		f.duty = randDuty()
	}

	// Pulse wave: HIGH for duty*cycleTicks ticks, LOW for remainder
	highTicks := int(f.duty * float64(cycleTicks))
	var base float64
	if cyclePos < transTicks {
		// Rising transition from previous state
		prev := f.threshold * 0.90
		next := f.threshold * 1.10
		if cyclePos < highTicks {
			base = lerp(prev, next, float64(cyclePos)/float64(transTicks))
		} else {
			base = lerp(next, prev, float64(cyclePos)/float64(transTicks))
		}
	} else if cyclePos < highTicks {
		base = f.threshold * 1.10 // HIGH plateau
	} else if cyclePos < highTicks+transTicks {
		base = lerp(f.threshold*1.10, f.threshold*0.90, float64(cyclePos-highTicks)/float64(transTicks))
	} else {
		base = f.threshold * 0.90 // LOW plateau
	}

	// Noise layer 1: Gaussian (σ = 5% of threshold)
	gauss := randNorm() * 0.05 * f.threshold
	// Noise layer 2: random spikes (P=3%, ±35%)
	spike := 0.0
	if rand.Float64() < 0.03 {
		spike = (rand.Float64()*2 - 1) * 0.35 * f.threshold
	}
	// Noise layer 3: slow sinusoidal drift (16h period, ±30%)
	f.driftPhase += 2 * math.Pi / (16 * 60) // 16h in minutes
	drift := math.Sin(f.driftPhase) * 0.30 * f.threshold * 0.10

	val := base + gauss + spike + drift

	// Hard clamp: ±18% of threshold
	lo := f.threshold * 0.82
	hi := f.threshold * 1.18
	if val < lo {
		val = lo
	}
	if val > hi {
		val = hi
	}
	return math.Round(val*100) / 100
}

func randDuty() float64 {
	// Random duty cycle between 30% and 70%
	return 0.30 + rand.Float64()*0.40
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

func randNorm() float64 {
	// Box-Muller approximation
	u1 := rand.Float64()
	u2 := rand.Float64()
	return math.Sqrt(-2*math.Log(u1+1e-9)) * math.Cos(2*math.Pi*u2)
}
