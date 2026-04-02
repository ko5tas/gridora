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

const (
	maxConsecutiveEmpty = 14 // Allow 2-week gaps (e.g., Zappi offline during installation)
	probeJumpDays       = 90 // When hitting empty streak, jump back and probe
)

// Backfiller fetches historical per-minute data going backwards in time.
type Backfiller struct {
	client    *myenergi.Client
	store     store.Store
	serial    string
	rateLimit time.Duration
	logger    *slog.Logger
}

// NewBackfiller creates a historical data backfiller for a specific Zappi serial.
func NewBackfiller(client *myenergi.Client, store store.Store, serial string, rateLimit time.Duration, logger *slog.Logger) *Backfiller {
	return &Backfiller{
		client:    client,
		store:     store,
		serial:    serial,
		rateLimit: rateLimit,
		logger:    logger,
	}
}

// Run fetches historical data going backwards from yesterday until the API
// returns empty data for maxConsecutiveEmpty consecutive days.
// It persists backfill state so it can resume after restarts.
func (b *Backfiller) Run(ctx context.Context) error {
	state, err := b.store.GetBackfillState(ctx, b.serial)
	if err != nil {
		return fmt.Errorf("reading backfill state: %w", err)
	}

	yesterday := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)

	if state == nil {
		state = &store.BackfillState{
			Serial:     b.serial,
			OldestDate: yesterday,
			NewestDate: yesterday,
			Status:     "in_progress",
		}
	}

	if state.Status == "complete" {
		b.logger.Info("backfill already complete", "serial", b.serial, "oldest", state.OldestDate.Format("2006-01-02"))
		// Still forward-fill any gaps
		return b.forwardFill(ctx, state, yesterday)
	}

	b.logger.Info("starting backfill",
		"serial", b.serial,
		"oldest_so_far", state.OldestDate.Format("2006-01-02"),
	)

	// Forward-fill: fill gaps between newest collected and yesterday
	if err := b.forwardFill(ctx, state, yesterday); err != nil {
		return err
	}

	// Backward-fill: walk backwards from oldest known date
	consecutiveEmpty := 0
	current := state.OldestDate.AddDate(0, 0, -1)

	for consecutiveEmpty < maxConsecutiveEmpty {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		records, err := b.fetchAndStore(ctx, current)
		if err != nil {
			b.logger.Error("backfill fetch failed", "date", current.Format("2006-01-02"), "error", err)
			state.LastError = err.Error()
			b.store.SaveBackfillState(ctx, state)
			// Continue to next day rather than stopping
			consecutiveEmpty++
			current = current.AddDate(0, 0, -1)
			time.Sleep(b.rateLimit)
			continue
		}

		if len(records) == 0 {
			consecutiveEmpty++
			b.logger.Info("no data for date",
				"date", current.Format("2006-01-02"),
				"consecutive_empty", consecutiveEmpty,
			)
		} else {
			consecutiveEmpty = 0
			state.OldestDate = current
			state.LastError = ""
			b.logger.Info("backfilled day",
				"date", current.Format("2006-01-02"),
				"records", len(records),
			)
		}

		state.UpdatedAt = time.Now().UTC()
		if err := b.store.SaveBackfillState(ctx, state); err != nil {
			b.logger.Error("failed to save backfill state", "error", err)
		}

		current = current.AddDate(0, 0, -1)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(b.rateLimit):
		}
	}

	// Before giving up, probe further back — there might be data beyond a gap
	if err := b.probeAndResume(ctx, state, current); err != nil {
		return err
	}

	state.Status = "complete"
	state.UpdatedAt = time.Now().UTC()
	b.store.SaveBackfillState(ctx, state)
	b.logger.Info("backfill complete",
		"serial", b.serial,
		"oldest_date", state.OldestDate.Format("2006-01-02"),
	)

	return nil
}

// probeAndResume checks if data exists further back by jumping in 90-day increments.
// If found, it backfills that region and everything in between.
func (b *Backfiller) probeAndResume(ctx context.Context, state *store.BackfillState, gapStart time.Time) error {
	probeDate := gapStart
	for {
		probeDate = probeDate.AddDate(0, 0, -probeJumpDays)

		b.logger.Info("probing for older data", "date", probeDate.Format("2006-01-02"))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(b.rateLimit):
		}

		apiRecords, err := b.client.ZappiDayMinute(ctx, b.serial, probeDate)
		if err != nil {
			b.logger.Warn("probe failed", "date", probeDate.Format("2006-01-02"), "error", err)
			return nil // Give up probing on error
		}

		if len(apiRecords) == 0 {
			b.logger.Info("no data at probe date, backfill truly complete", "probed", probeDate.Format("2006-01-02"))
			return nil
		}

		b.logger.Info("found older data, resuming backfill", "probe_date", probeDate.Format("2006-01-02"), "records", len(apiRecords))

		// Backfill from gapStart backwards to probeDate (and beyond)
		current := gapStart.AddDate(0, 0, -1)
		consecutiveEmpty := 0
		for !current.Before(probeDate.AddDate(0, 0, -maxConsecutiveEmpty)) {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			records, err := b.fetchAndStore(ctx, current)
			if err != nil {
				b.logger.Error("backfill fetch failed", "date", current.Format("2006-01-02"), "error", err)
				consecutiveEmpty++
				current = current.AddDate(0, 0, -1)
				time.Sleep(b.rateLimit)
				continue
			}

			if len(records) == 0 {
				consecutiveEmpty++
			} else {
				consecutiveEmpty = 0
				state.OldestDate = current
				b.logger.Info("backfilled day", "date", current.Format("2006-01-02"), "records", len(records))
			}

			state.UpdatedAt = time.Now().UTC()
			b.store.SaveBackfillState(ctx, state)

			current = current.AddDate(0, 0, -1)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(b.rateLimit):
			}
		}

		// Update gapStart for the next probe cycle
		gapStart = current
	}
}

// forwardFill fills any gaps between the newest collected date and yesterday.
func (b *Backfiller) forwardFill(ctx context.Context, state *store.BackfillState, yesterday time.Time) error {
	current := state.NewestDate.AddDate(0, 0, 1)
	for !current.After(yesterday) {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		records, err := b.fetchAndStore(ctx, current)
		if err != nil {
			b.logger.Error("forward-fill failed", "date", current.Format("2006-01-02"), "error", err)
		} else if len(records) > 0 {
			state.NewestDate = current
			b.logger.Info("forward-filled day", "date", current.Format("2006-01-02"), "records", len(records))
		}

		state.UpdatedAt = time.Now().UTC()
		b.store.SaveBackfillState(ctx, state)

		current = current.AddDate(0, 0, 1)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(b.rateLimit):
		}
	}
	return nil
}

// fetchAndStore fetches minute data for a date, stores it, and aggregates hourly/daily.
func (b *Backfiller) fetchAndStore(ctx context.Context, date time.Time) ([]store.MinuteRecord, error) {
	apiRecords, err := b.client.ZappiDayMinute(ctx, b.serial, date)
	if err != nil {
		return nil, err
	}

	if len(apiRecords) == 0 {
		return nil, nil
	}

	// Convert API records to store records
	storeRecords := make([]store.MinuteRecord, 0, len(apiRecords))
	for _, r := range apiRecords {
		ts := time.Date(r.Year, time.Month(r.Month), r.Day, r.Hour, r.Minute, 0, 0, time.UTC)
		storeRecords = append(storeRecords, store.MinuteRecord{
			Serial:    b.serial,
			Timestamp: ts,
			ImportJ:   r.Import,
			ExportJ:   r.Export,
			GenPosJ:   r.GenPos,
			GenNegJ:   r.GenNeg,
			H1DJ:      r.H1D,
			H2DJ:      r.H2D,
			H3DJ:      r.H3D,
			H1BJ:      r.H1B,
			H2BJ:      r.H2B,
			H3BJ:      r.H3B,
			Voltage:   energy.DeciVoltsToVolts(r.Voltage),
			Frequency: r.Frequency,
		})
	}

	if err := b.store.UpsertMinuteRecords(ctx, storeRecords); err != nil {
		return nil, fmt.Errorf("storing minute records: %w", err)
	}

	// Aggregate hourly and daily
	hourly := energy.AggregateHourly(b.serial, storeRecords)
	if err := b.store.UpsertHourlyRecords(ctx, hourly); err != nil {
		b.logger.Error("failed to store hourly aggregates", "error", err)
	}

	daily := energy.AggregateDaily(b.serial, date, storeRecords)
	if err := b.store.UpsertDailyRecord(ctx, daily); err != nil {
		b.logger.Error("failed to store daily aggregate", "error", err)
	}

	return storeRecords, nil
}
