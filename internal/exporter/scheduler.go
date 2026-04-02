package exporter

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ko5tas/gridora/internal/store"
)

// Scheduler runs exports on a configured schedule.
type Scheduler struct {
	exporter *Exporter
	store    store.Store
	time     string // "HH:MM"
	dbBackup bool
	logger   *slog.Logger
}

// SchedulerConfig holds export scheduler configuration.
type SchedulerConfig struct {
	Path     string
	Time     string
	DBBackup bool
}

// NewScheduler creates an export scheduler.
func NewScheduler(store store.Store, cfg SchedulerConfig, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		exporter: NewExporter(store, cfg.Path, logger),
		store:    store,
		time:     cfg.Time,
		dbBackup: cfg.DBBackup,
		logger:   logger,
	}
}

// Run starts the export loop. Blocks until ctx is cancelled.
// On first run, it catches up by exporting all days since the last export.
func (s *Scheduler) Run(ctx context.Context) {
	// Run an initial export immediately to catch up
	s.runExport(ctx)

	for {
		now := time.Now()
		next := nextRun(now, s.time)
		s.logger.Info("next export", "at", next.Format("2006-01-02T15:04:05"))

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
		}

		s.runExport(ctx)
	}
}

// runExport performs a full export cycle: daily CSVs, monthly CSVs, and DB backup.
func (s *Scheduler) runExport(ctx context.Context) {
	yesterday := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -1)

	// Discover serials from the database each time — the collector may have added new ones
	serials, err := s.store.Serials(ctx)
	if err != nil {
		s.logger.Error("failed to get serials for export", "error", err)
		return
	}
	if len(serials) == 0 {
		s.logger.Info("no serials found yet, skipping export")
		return
	}

	for _, serial := range serials {
		if ctx.Err() != nil {
			return
		}
		s.exportDaily(ctx, serial, yesterday)
		s.exportMonthly(ctx, serial, yesterday)
	}

	if s.dbBackup {
		if err := s.exporter.BackupDB(ctx); err != nil {
			s.logger.Error("db backup failed", "error", err)
		}
	}
}

// exportDaily exports all days from last exported date up to yesterday.
func (s *Scheduler) exportDaily(ctx context.Context, serial string, yesterday time.Time) {
	state, err := s.store.GetExportState(ctx, "daily:"+serial)
	if err != nil {
		s.logger.Error("failed to get export state", "error", err)
		return
	}

	// Start from the day after the last export, or 30 days back on first run
	var start time.Time
	if state != nil {
		start = state.LastDate.AddDate(0, 0, 1)
	} else {
		start = yesterday.AddDate(0, 0, -30)
	}

	if start.After(yesterday) {
		return // Already up to date
	}

	exported := 0
	current := start
	for !current.After(yesterday) {
		if ctx.Err() != nil {
			return
		}

		if err := s.exporter.ExportDay(ctx, serial, current); err != nil {
			s.logger.Error("daily export failed", "serial", serial, "date", current.Format("2006-01-02"), "error", err)
		} else {
			exported++
		}

		// Update state after each day so we don't re-export on restart
		s.store.SaveExportState(ctx, &store.ExportState{
			ExportType: "daily:" + serial,
			LastDate:   current,
		})

		current = current.AddDate(0, 0, 1)
	}

	if exported > 0 {
		s.logger.Info("daily export complete", "serial", serial, "days", exported)
	}
}

// exportMonthly exports the previous month's summary (runs once per month).
func (s *Scheduler) exportMonthly(ctx context.Context, serial string, yesterday time.Time) {
	state, err := s.store.GetExportState(ctx, "monthly:"+serial)
	if err != nil {
		s.logger.Error("failed to get export state", "error", err)
		return
	}

	// Export all months from the last exported month up to the previous complete month
	prevMonth := yesterday.AddDate(0, 0, -yesterday.Day()+1) // First of yesterday's month
	lastCompleteMonth := prevMonth.AddDate(0, -1, 0)         // First of the month before

	var start time.Time
	if state != nil {
		start = state.LastDate.AddDate(0, 1, 0) // Month after last export
	} else {
		start = lastCompleteMonth.AddDate(0, -6, 0) // 6 months back on first run
	}

	if start.After(lastCompleteMonth) {
		return
	}

	current := start
	for !current.After(lastCompleteMonth) {
		if ctx.Err() != nil {
			return
		}

		if err := s.exporter.ExportMonth(ctx, serial, current.Year(), current.Month()); err != nil {
			s.logger.Error("monthly export failed", "serial", serial, "month", current.Format("2006-01"), "error", err)
		}

		s.store.SaveExportState(ctx, &store.ExportState{
			ExportType: "monthly:" + serial,
			LastDate:   current,
		})

		current = current.AddDate(0, 1, 0)
	}
}

// nextRun calculates the next occurrence of the given HH:MM time.
func nextRun(now time.Time, timeStr string) time.Time {
	var hour, min int
	fmt.Sscanf(timeStr, "%d:%d", &hour, &min)

	next := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
