package collector

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ko5tas/gridora/internal/energy"
	"github.com/ko5tas/gridora/internal/myenergi"
	"github.com/ko5tas/gridora/internal/store"
)

// Scheduler orchestrates periodic data collection from the myenergi API.
type Scheduler struct {
	client              *myenergi.Client
	store               store.Store
	pollInterval        time.Duration
	backfillRateLimit   time.Duration
	dailyCollectionTime string // "HH:MM"
	backfillOnStartup   bool
	logger              *slog.Logger
}

// SchedulerConfig holds configuration for the scheduler.
type SchedulerConfig struct {
	PollInterval        time.Duration
	BackfillRateLimit   time.Duration
	DailyCollectionTime string
	BackfillOnStartup   bool
}

// NewScheduler creates a data collection scheduler.
func NewScheduler(client *myenergi.Client, store store.Store, cfg SchedulerConfig, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		client:              client,
		store:               store,
		pollInterval:        cfg.PollInterval,
		backfillRateLimit:   cfg.BackfillRateLimit,
		dailyCollectionTime: cfg.DailyCollectionTime,
		backfillOnStartup:   cfg.BackfillOnStartup,
		logger:              logger,
	}
}

// Run starts all collection loops. Blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) error {
	// Discover Zappi serial(s) from the first status poll
	serials, err := s.discoverSerials(ctx)
	if err != nil {
		return fmt.Errorf("discovering zappi serials: %w", err)
	}

	// Start backfill in background if enabled
	if s.backfillOnStartup {
		for _, serial := range serials {
			serial := serial
			go func() {
				bf := NewBackfiller(s.client, s.store, serial, s.backfillRateLimit, s.logger)
				if err := bf.Run(ctx); err != nil && ctx.Err() == nil {
					s.logger.Error("backfill failed", "serial", serial, "error", err)
				}
			}()
		}
	}

	// Start daily collection checker in background
	go s.dailyCollectionLoop(ctx, serials)

	// Start recent-day refresh in background (keeps today + yesterday up to date)
	go s.recentRefreshLoop(ctx, serials)

	// Run real-time status polling in foreground
	return s.statusLoop(ctx)
}

// discoverSerials does an initial status poll to find connected Zappi serial numbers.
func (s *Scheduler) discoverSerials(ctx context.Context) ([]string, error) {
	statuses, err := s.client.ZappiStatus(ctx)
	if err != nil {
		return nil, err
	}

	serials := make([]string, 0, len(statuses))
	for _, z := range statuses {
		serials = append(serials, z.SerialNumber.String())
		s.logger.Info("discovered zappi", "serial", z.SerialNumber.String())
	}

	if len(serials) == 0 {
		return nil, fmt.Errorf("no zappi devices found on hub")
	}

	return serials, nil
}

// statusLoop polls real-time status every pollInterval.
func (s *Scheduler) statusLoop(ctx context.Context) error {
	s.logger.Info("starting status polling", "interval", s.pollInterval)

	s.pollStatus(ctx)

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("status polling stopped")
			return ctx.Err()
		case <-ticker.C:
			s.pollStatus(ctx)
		}
	}
}

// dailyCollectionLoop waits for the configured time each day, then fetches
// yesterday's per-minute data and aggregates it.
func (s *Scheduler) dailyCollectionLoop(ctx context.Context, serials []string) {
	for {
		now := time.Now()
		next := nextDailyRun(now, s.dailyCollectionTime)
		s.logger.Info("next daily collection", "at", next.Format(time.RFC3339))

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
		}

		yesterday := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
		for _, serial := range serials {
			s.logger.Info("collecting daily data", "serial", serial, "date", yesterday.Format("2006-01-02"))

			bf := NewBackfiller(s.client, s.store, serial, s.backfillRateLimit, s.logger)
			if _, err := bf.fetchAndStore(ctx, yesterday); err != nil {
				s.logger.Error("daily collection failed", "serial", serial, "error", err)
			}
		}
	}
}

// nextDailyRun calculates the next occurrence of the given HH:MM time.
func nextDailyRun(now time.Time, timeStr string) time.Time {
	var hour, min int
	fmt.Sscanf(timeStr, "%d:%d", &hour, &min)

	next := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// recentRefreshLoop periodically re-fetches today's partial data and
// yesterday if it looks incomplete. This fills the gap between the
// backwards backfill and the next-day daily collection.
func (s *Scheduler) recentRefreshLoop(ctx context.Context, serials []string) {
	const (
		refreshInterval     = 5 * time.Minute
		minExpectedRecords  = 1000 // A full day has ~1440; anything below this gets re-fetched
	)

	s.logger.Info("starting recent-day refresh", "interval", refreshInterval)

	// Run immediately, then on a timer
	s.refreshRecentDays(ctx, serials, minExpectedRecords)

	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshRecentDays(ctx, serials, minExpectedRecords)
		}
	}
}

func (s *Scheduler) refreshRecentDays(ctx context.Context, serials []string, minRecords int) {
	now := time.Now().UTC()
	today := now.Truncate(24 * time.Hour)
	yesterday := today.AddDate(0, 0, -1)

	for _, serial := range serials {
		if ctx.Err() != nil {
			return
		}

		bf := NewBackfiller(s.client, s.store, serial, s.backfillRateLimit, s.logger)

		// Always refresh today (partial data, grows throughout the day)
		if _, err := bf.fetchAndStore(ctx, today); err != nil {
			s.logger.Error("today refresh failed", "serial", serial, "error", err)
		}

		// Re-fetch yesterday if it looks incomplete
		recs, err := s.store.MinuteRecords(ctx, serial, yesterday, today)
		if err == nil && len(recs) < minRecords {
			s.logger.Info("re-fetching incomplete yesterday",
				"serial", serial,
				"current_records", len(recs),
			)
			if _, err := bf.fetchAndStore(ctx, yesterday); err != nil {
				s.logger.Error("yesterday refresh failed", "serial", serial, "error", err)
			}
		}
	}
}

func (s *Scheduler) pollStatus(ctx context.Context) {
	statuses, err := s.client.ZappiStatus(ctx)
	if err != nil {
		s.logger.Error("failed to fetch zappi status", "error", err)
		return
	}

	now := time.Now().UTC()
	for _, z := range statuses {
		rec := &store.StatusRecord{
			Serial:         z.SerialNumber.String(),
			Timestamp:      now,
			GridW:          z.Grid,
			GenerationW:    z.Generation,
			DiversionW:     z.Diversion,
			Voltage:        energy.DeciVoltsToVolts(z.Voltage),
			Frequency:      z.Frequency,
			ChargeAddedKWh: z.ChargeAdded,
			ZappiMode:      z.Mode,
			Status:         z.Status,
			PlugStatus:     z.PlugStatus,
			ECTP1W:         z.ECTP1,
			ECTP2W:         z.ECTP2,
			ECTP3W:         z.ECTP3,
			ECTT1:          z.ECTT1,
			ECTT2:          z.ECTT2,
			ECTT3:          z.ECTT3,
		}

		if err := s.store.InsertStatus(ctx, rec); err != nil {
			s.logger.Error("failed to store status", "serial", z.SerialNumber.String(), "error", err)
			continue
		}

		s.logger.Info("status recorded",
			"serial", z.SerialNumber.String(),
			"grid", fmt.Sprintf("%.0fW", z.Grid),
			"gen", fmt.Sprintf("%.0fW", z.Generation),
			"div", fmt.Sprintf("%.0fW", z.Diversion),
			"voltage", fmt.Sprintf("%.1fV", energy.DeciVoltsToVolts(z.Voltage)),
		)
	}
}
