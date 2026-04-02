package exporter

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ko5tas/gridora/internal/energy"
	"github.com/ko5tas/gridora/internal/store"
)

const (
	keepDaily   = 30 // Daily backups retained
	keepMonthly = 12 // Monthly backups per year retained
)

// Exporter writes CSV exports and DB backups to a configured path.
type Exporter struct {
	store  store.Store
	path   string
	logger *slog.Logger
}

// NewExporter creates an exporter that writes to the given base path.
func NewExporter(store store.Store, path string, logger *slog.Logger) *Exporter {
	return &Exporter{
		store:  store,
		path:   path,
		logger: logger,
	}
}

// ExportDay writes a CSV file with minute-level data for a single date.
// Safety: never overwrites an existing file with fewer records.
// Output: {path}/daily/{serial}/{YYYY-MM-DD}.csv
func (e *Exporter) ExportDay(ctx context.Context, serial string, date time.Time) error {
	date = date.Truncate(24 * time.Hour)
	nextDay := date.AddDate(0, 0, 1)

	records, err := e.store.MinuteRecords(ctx, serial, date, nextDay)
	if err != nil {
		return fmt.Errorf("querying minute records: %w", err)
	}

	if len(records) == 0 {
		return nil // Nothing to export
	}

	dir := filepath.Join(e.path, "daily", serial)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating export dir: %w", err)
	}

	filePath := filepath.Join(dir, date.Format("2006-01-02")+".csv")

	// Safety: don't overwrite if existing file has more data
	if existingRows := countCSVRows(filePath); existingRows > len(records) {
		e.logger.Warn("skipping export — existing file has more records",
			"file", filePath, "existing", existingRows, "new", len(records))
		return nil
	}

	// Write to a temp file first, then rename (atomic on same filesystem)
	tmpPath := filePath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("creating temp CSV file: %w", err)
	}

	w := csv.NewWriter(f)

	w.Write([]string{
		"timestamp", "import_kwh", "export_kwh", "generation_kwh",
		"diverted_kwh", "boosted_kwh", "voltage", "frequency",
	})

	for _, r := range records {
		w.Write([]string{
			r.Timestamp.Format("2006-01-02T15:04:05Z"),
			fmt.Sprintf("%.6f", energy.JoulesToKWh(r.ImportJ)),
			fmt.Sprintf("%.6f", energy.JoulesToKWh(r.ExportJ)),
			fmt.Sprintf("%.6f", energy.JoulesToKWh(r.GenPosJ)),
			fmt.Sprintf("%.6f", energy.JoulesToKWh(r.H1DJ+r.H2DJ+r.H3DJ)),
			fmt.Sprintf("%.6f", energy.JoulesToKWh(r.H1BJ+r.H2BJ+r.H3BJ)),
			fmt.Sprintf("%.1f", r.Voltage),
			fmt.Sprintf("%.2f", r.Frequency),
		})
	}

	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing CSV: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing CSV: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming temp file: %w", err)
	}

	e.logger.Info("exported daily CSV", "serial", serial, "date", date.Format("2006-01-02"), "records", len(records))
	return nil
}

// ExportMonth writes a CSV file with daily summaries for a calendar month.
// Safety: never overwrites an existing file with fewer records.
// Output: {path}/monthly/{serial}/{YYYY-MM}.csv
func (e *Exporter) ExportMonth(ctx context.Context, serial string, year int, month time.Month) error {
	from := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)

	records, err := e.store.DailyRecords(ctx, serial, from, to)
	if err != nil {
		return fmt.Errorf("querying daily records: %w", err)
	}

	if len(records) == 0 {
		return nil
	}

	dir := filepath.Join(e.path, "monthly", serial)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating export dir: %w", err)
	}

	filePath := filepath.Join(dir, from.Format("2006-01")+".csv")

	if existingRows := countCSVRows(filePath); existingRows > len(records) {
		e.logger.Warn("skipping export — existing file has more records",
			"file", filePath, "existing", existingRows, "new", len(records))
		return nil
	}

	tmpPath := filePath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("creating temp CSV file: %w", err)
	}

	w := csv.NewWriter(f)

	w.Write([]string{
		"date", "import_kwh", "export_kwh", "generation_kwh",
		"diverted_kwh", "boosted_kwh", "self_consumption_pct",
		"peak_generation_w", "peak_import_w",
	})

	for _, r := range records {
		w.Write([]string{
			r.Date.Format("2006-01-02"),
			fmt.Sprintf("%.3f", r.ImportKWh),
			fmt.Sprintf("%.3f", r.ExportKWh),
			fmt.Sprintf("%.3f", r.GenerationKWh),
			fmt.Sprintf("%.3f", r.DivertedKWh),
			fmt.Sprintf("%.3f", r.BoostedKWh),
			fmt.Sprintf("%.1f", r.SelfConsumptionPct),
			fmt.Sprintf("%.0f", r.PeakGenerationW),
			fmt.Sprintf("%.0f", r.PeakImportW),
		})
	}

	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing CSV: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing CSV: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming temp file: %w", err)
	}

	e.logger.Info("exported monthly CSV", "serial", serial, "month", from.Format("2006-01"), "days", len(records))
	return nil
}

// BackupDB creates a gzip-compressed database snapshot with tiered retention.
//
// Strategy (Grandfather-Father-Son):
//   - Daily:   {path}/db/daily/gridora-YYYY-MM-DD.db.gz   — last 30 kept
//   - Monthly: {path}/db/monthly/gridora-YYYY-MM.db.gz     — last 12 kept
//   - Yearly:  {path}/db/yearly/gridora-YYYY.db.gz         — kept indefinitely
//
// On the 1st of the month the daily backup is promoted to monthly.
// On 1st January the monthly backup is promoted to yearly.
func (e *Exporter) BackupDB(ctx context.Context) error {
	now := time.Now().UTC()
	dbPath := e.store.DBPath()

	dailyDir := filepath.Join(e.path, "db", "daily")
	monthlyDir := filepath.Join(e.path, "db", "monthly")
	yearlyDir := filepath.Join(e.path, "db", "yearly")

	for _, d := range []string{dailyDir, monthlyDir, yearlyDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("creating backup dir %s: %w", d, err)
		}
	}

	// 1. Create daily backup: VACUUM INTO → integrity check → gzip
	dateStr := now.Format("2006-01-02")
	rawPath := filepath.Join(dailyDir, "gridora-"+dateStr+".db")
	gzPath := rawPath + ".gz"

	if err := vacuumInto(ctx, dbPath, rawPath); err != nil {
		return fmt.Errorf("database backup: %w", err)
	}

	if err := integrityCheck(ctx, rawPath); err != nil {
		os.Remove(rawPath)
		return fmt.Errorf("backup integrity check failed: %w", err)
	}

	compressedSize, err := gzipFile(rawPath, gzPath)
	if err != nil {
		return fmt.Errorf("compressing backup: %w", err)
	}

	e.logger.Info("daily backup complete",
		"file", gzPath,
		"size_mb", fmt.Sprintf("%.1f", float64(compressedSize)/1024/1024),
	)

	// 2. Promote to monthly on the 1st of the month
	if now.Day() == 1 {
		monthFile := filepath.Join(monthlyDir, "gridora-"+now.Format("2006-01")+".db.gz")
		if err := copyFile(gzPath, monthFile); err != nil {
			e.logger.Error("monthly promotion failed", "error", err)
		} else {
			e.logger.Info("promoted to monthly backup", "file", monthFile)
		}

		// 3. Promote to yearly on 1st January
		if now.Month() == time.January {
			yearFile := filepath.Join(yearlyDir, "gridora-"+now.Format("2006")+".db.gz")
			if err := copyFile(gzPath, yearFile); err != nil {
				e.logger.Error("yearly promotion failed", "error", err)
			} else {
				e.logger.Info("promoted to yearly backup", "file", yearFile)
			}
		}
	}

	// 4. Prune old backups
	pruneOldFiles(e.logger, dailyDir, "gridora-*.db.gz", keepDaily)
	pruneOldFiles(e.logger, monthlyDir, "gridora-*.db.gz", keepMonthly)
	// Yearly backups are kept indefinitely

	return nil
}

// pruneOldFiles keeps only the newest `keep` files matching pattern in dir.
func pruneOldFiles(logger *slog.Logger, dir, pattern string, keep int) {
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil || len(matches) <= keep {
		return
	}

	// Filenames contain dates so lexicographic sort = chronological
	toRemove := matches[:len(matches)-keep]
	for _, f := range toRemove {
		if err := os.Remove(f); err != nil {
			logger.Warn("failed to remove old backup", "file", f, "error", err)
		} else {
			logger.Info("pruned old backup", "file", filepath.Base(f))
		}
	}
}

// copyFile copies src to dst. Used for tier promotion.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		os.Remove(dst)
		return err
	}
	return out.Close()
}

// countCSVRows returns the number of data rows (excluding header) in a CSV file.
// Returns 0 if the file doesn't exist or can't be read.
func countCSVRows(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	count := -1 // Start at -1 to exclude the header row
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		count++
	}
	if count < 0 {
		return 0
	}
	return count
}
