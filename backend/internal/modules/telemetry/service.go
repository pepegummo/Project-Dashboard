package telemetry

import (
	"context"
	"fmt"
	"iot-dashboard/internal/middleware"
	"iot-dashboard/internal/modules/machines"
	"regexp"
	"strconv"
	"time"
)

// TIME_RANGE_PRESETS maps range strings to milliseconds — same as TypeScript version.
var timeRangePresets = map[string]time.Duration{
	"5m":  5 * time.Minute,
	"15m": 15 * time.Minute,
	"30m": 30 * time.Minute,
	"1h":  1 * time.Hour,
	"6h":  6 * time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"15d": 15 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
	"3mo": 90 * 24 * time.Hour,
	"6mo": 180 * 24 * time.Hour,
	"1y":  365 * 24 * time.Hour,
}

// BUCKET_FOR_RANGE — must keep bucket < 1 pulse cycle (120 min) to preserve pulse shape.
var bucketForRange = map[string]string{
	"5m":  "1 minute",
	"15m": "1 minute",
	"30m": "1 minute",
	"1h":  "1 minute",
	"6h":  "5 minutes",
	"24h": "15 minutes",
	"7d":  "30 minutes",
	"15d": "30 minutes",
	"30d": "1 hour",
	"3mo": "1 hour",
	"6mo": "1 hour",
	"1y":  "1 hour",
}

type Service struct {
	repo        *Repository
	machineRepo *machines.Repository
}

func NewService() *Service {
	return &Service{
		repo:        &Repository{},
		machineRepo: &machines.Repository{},
	}
}

func (s *Service) requireMachineInOrg(ctx context.Context, machineID, orgID string) error {
	m, err := s.machineRepo.FindByID(ctx, machineID)
	if err != nil || m.ProductionLine.Factory.OrganizationID != orgID {
		return middleware.NewAppError(404, "NOT_FOUND", "Machine not found")
	}
	return nil
}

func (s *Service) Ingest(ctx context.Context, machineID string, data map[string]interface{}, orgID string) (map[string]interface{}, error) {
	if err := s.requireMachineInOrg(ctx, machineID, orgID); err != nil {
		return nil, err
	}
	now := time.Now()
	if err := s.repo.Ingest(ctx, machineID, data, now); err != nil {
		return nil, err
	}
	_ = s.machineRepo.UpdateStatus(ctx, machineID, "online")
	return map[string]interface{}{"machineId": machineID, "timestamp": now, "data": data}, nil
}

func (s *Service) GetLatest(ctx context.Context, machineID string, orgID *string) (*LatestSnapshot, error) {
	if orgID != nil {
		if err := s.requireMachineInOrg(ctx, machineID, *orgID); err != nil {
			return nil, err
		}
	}
	return s.repo.GetLatest(ctx, machineID)
}

// calculateBucketForDuration picks a TimescaleDB bucket size for an arbitrary time range.
func calculateBucketForDuration(d time.Duration) string {
	switch {
	case d <= 1*time.Hour:
		return "1 minute"
	case d <= 6*time.Hour:
		return "5 minutes"
	case d <= 24*time.Hour:
		return "15 minutes"
	case d <= 15*24*time.Hour:
		return "30 minutes"
	default:
		return "1 hour"
	}
}

func (s *Service) GetSeries(ctx context.Context, machineID, field, timeRange, startTimeStr, endTimeStr string, orgID *string) (map[string]interface{}, error) {
	if orgID != nil {
		if err := s.requireMachineInOrg(ctx, machineID, *orgID); err != nil {
			return nil, err
		}
	}

	var from, to time.Time
	var bucket string

	if startTimeStr != "" && endTimeStr != "" {
		var errFrom, errTo error
		from, errFrom = time.Parse(time.RFC3339, startTimeStr)
		to, errTo = time.Parse(time.RFC3339, endTimeStr)
		if errFrom != nil || errTo != nil {
			return nil, middleware.NewAppError(400, "VALIDATION_ERROR", "Invalid startTime or endTime format — use RFC3339")
		}
		bucket = calculateBucketForDuration(to.Sub(from))
	} else {
		rangeD, ok := timeRangePresets[timeRange]
		if !ok {
			rangeD = timeRangePresets["1h"]
		}
		bucket, _ = bucketForRange[timeRange]
		if bucket == "" {
			bucket = "1 minute"
		}
		to = time.Now()
		from = to.Add(-rangeD)
	}

	data, err := s.repo.GetTimescaleAggregate(ctx, machineID, field, from, to, bucket)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"machineId": machineID, "field": field,
		"from": from, "to": to, "data": data,
	}, nil
}

func (s *Service) GetAggregate(ctx context.Context, machineID, field, period, orgID string) (map[string]interface{}, error) {
	if err := s.requireMachineInOrg(ctx, machineID, orgID); err != nil {
		return nil, err
	}
	rangeD, ok := timeRangePresets[period]
	if !ok {
		rangeD = timeRangePresets["1h"]
	}
	to := time.Now()
	from := to.Add(-rangeD)
	summary, err := s.repo.GetAggregateSummary(ctx, machineID, field, from, to)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"machineId": machineID, "field": field,
		"period": period, "from": from, "to": to, "summary": summary,
	}, nil
}

func (s *Service) GetDailyCount(ctx context.Context, machineID string, days int, orgID *string) (map[string]interface{}, error) {
	if orgID != nil {
		if err := s.requireMachineInOrg(ctx, machineID, *orgID); err != nil {
			return nil, err
		}
	}
	data, err := s.repo.GetDailyCount(ctx, machineID, days)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"machineId": machineID, "days": days, "data": data}, nil
}

func (s *Service) GetHourlyCount(ctx context.Context, machineID string, hours int, orgID *string) (map[string]interface{}, error) {
	if orgID != nil {
		if err := s.requireMachineInOrg(ctx, machineID, *orgID); err != nil {
			return nil, err
		}
	}
	data, err := s.repo.GetHourlyCount(ctx, machineID, hours)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"machineId": machineID, "hours": hours, "data": data}, nil
}

// bucketRe keeps the client-supplied bucket string out of the SQL unless it matches a strict shape —
// the count query interpolates the interval literal rather than binding it.
var bucketRe = regexp.MustCompile(`^(\d{1,4})(m|h|d)$`)

// normalizeStatus validates the count status filter ("" defaults to "all").
func normalizeStatus(s string) (string, error) {
	switch s {
	case "", "all":
		return "all", nil
	case "good", "reject":
		return s, nil
	default:
		return "", middleware.NewAppError(400, "VALIDATION_ERROR", "Invalid status — use all, good, or reject")
	}
}

// parseBucket converts "30m" / "2h" / "1d" into a safe Postgres interval string and its duration.
// Injection-safe: the number is a validated int and the unit is from a fixed whitelist, so the
// result never carries arbitrary client text.
func parseBucket(s string) (interval string, every time.Duration, err error) {
	m := bucketRe.FindStringSubmatch(s)
	if m == nil {
		return "", 0, middleware.NewAppError(400, "VALIDATION_ERROR", "Invalid bucket — use <number><m|h|d>, e.g. 30m, 2h, 1d")
	}
	n, _ := strconv.Atoi(m[1])
	if n < 1 {
		return "", 0, middleware.NewAppError(400, "VALIDATION_ERROR", "Bucket number must be >= 1")
	}
	switch m[2] {
	case "m":
		return fmt.Sprintf("%d minutes", n), time.Duration(n) * time.Minute, nil
	case "h":
		return fmt.Sprintf("%d hours", n), time.Duration(n) * time.Hour, nil
	default: // "d"
		return fmt.Sprintf("%d days", n), time.Duration(n) * 24 * time.Hour, nil
	}
}

func (s *Service) GetBucketCount(ctx context.Context, machineID, sku, status, bucket string, points int, orgID *string) (map[string]interface{}, error) {
	if orgID != nil {
		if err := s.requireMachineInOrg(ctx, machineID, *orgID); err != nil {
			return nil, err
		}
	}
	status, err := normalizeStatus(status)
	if err != nil {
		return nil, err
	}
	if len(sku) > 64 {
		return nil, middleware.NewAppError(400, "VALIDATION_ERROR", "sku too long")
	}
	interval, every, err := parseBucket(bucket)
	if err != nil {
		return nil, err
	}
	if points < 1 {
		points = 48
	}
	if points > 500 {
		points = 500
	}
	to := time.Now()
	from := to.Add(-every * time.Duration(points))
	data, err := s.repo.GetSkuCount(ctx, machineID, sku, status, interval, from, to)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"machineId": machineID, "sku": sku, "status": status, "bucket": bucket,
		"from": from, "to": to, "data": data,
	}, nil
}

func (s *Service) GetMachineSkus(ctx context.Context, machineID string, orgID *string) ([]string, error) {
	if orgID != nil {
		if err := s.requireMachineInOrg(ctx, machineID, *orgID); err != nil {
			return nil, err
		}
	}
	from := time.Now().AddDate(0, 0, -30)
	return s.repo.GetMachineSkus(ctx, machineID, from)
}

func (s *Service) GetTotalCount(ctx context.Context, machineID string, orgID *string) (*TotalCount, error) {
	if orgID != nil {
		if err := s.requireMachineInOrg(ctx, machineID, *orgID); err != nil {
			return nil, err
		}
	}
	return s.repo.GetTotalCount(ctx, machineID)
}

func (s *Service) GetMultiMachineLatest(ctx context.Context, machineIDs []string, orgID *string) (map[string]*LatestSnapshot, error) {
	if orgID != nil {
		all, err := s.machineRepo.FindAll(ctx, *orgID, nil)
		if err != nil {
			return nil, err
		}
		ownedSet := make(map[string]struct{})
		for _, m := range all {
			ownedSet[m.ID] = struct{}{}
		}
		filtered := machineIDs[:0]
		for _, id := range machineIDs {
			if _, ok := ownedSet[id]; ok {
				filtered = append(filtered, id)
			}
		}
		machineIDs = filtered
	}
	return s.repo.GetLatestForMachines(ctx, machineIDs)
}
