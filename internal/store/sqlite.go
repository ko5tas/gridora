package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	db   *sql.DB
	path string
}

// NewSQLiteStore opens or creates a SQLite database at the given path.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Single writer, multiple readers
	db.SetMaxOpenConns(1)

	return &SQLiteStore{db: db, path: path}, nil
}

func (s *SQLiteStore) Migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schema)
	if err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	// Record schema version
	_, err = s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO schema_version (version, applied_at) VALUES (1, ?)`,
		time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) DBPath() string {
	return s.path
}

func (s *SQLiteStore) InsertStatus(ctx context.Context, rec *StatusRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO zappi_status
			(serial, timestamp, grid_w, generation_w, diversion_w, voltage, frequency,
			 charge_added_kwh, zappi_mode, status, plug_status,
			 ectp1_w, ectp2_w, ectp3_w, ectt1, ectt2, ectt3)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.Serial, rec.Timestamp.UTC().Format(time.RFC3339),
		rec.GridW, rec.GenerationW, rec.DiversionW,
		rec.Voltage, rec.Frequency, rec.ChargeAddedKWh,
		rec.ZappiMode, rec.Status, rec.PlugStatus,
		rec.ECTP1W, rec.ECTP2W, rec.ECTP3W,
		rec.ECTT1, rec.ECTT2, rec.ECTT3,
	)
	return err
}

func (s *SQLiteStore) LatestStatus(ctx context.Context, serial string) (*StatusRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT serial, timestamp, grid_w, generation_w, diversion_w, voltage, frequency,
			   charge_added_kwh, zappi_mode, status, plug_status,
			   ectp1_w, ectp2_w, ectp3_w, ectt1, ectt2, ectt3
		FROM zappi_status
		WHERE serial = ?
		ORDER BY timestamp DESC
		LIMIT 1`, serial)

	rec := &StatusRecord{}
	var ts string
	err := row.Scan(
		&rec.Serial, &ts, &rec.GridW, &rec.GenerationW, &rec.DiversionW,
		&rec.Voltage, &rec.Frequency, &rec.ChargeAddedKWh,
		&rec.ZappiMode, &rec.Status, &rec.PlugStatus,
		&rec.ECTP1W, &rec.ECTP2W, &rec.ECTP3W,
		&rec.ECTT1, &rec.ECTT2, &rec.ECTT3,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rec.Timestamp, _ = time.Parse(time.RFC3339, ts)
	return rec, nil
}

func (s *SQLiteStore) UpsertMinuteRecords(ctx context.Context, records []MinuteRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO zappi_minute
			(serial, timestamp, import_j, export_j, gen_pos_j, gen_neg_j,
			 h1d_j, h2d_j, h3d_j, h1b_j, h2b_j, h3b_j, voltage, frequency)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, r := range records {
		_, err := stmt.ExecContext(ctx,
			r.Serial, r.Timestamp.UTC().Format(time.RFC3339),
			r.ImportJ, r.ExportJ, r.GenPosJ, r.GenNegJ,
			r.H1DJ, r.H2DJ, r.H3DJ, r.H1BJ, r.H2BJ, r.H3BJ,
			r.Voltage, r.Frequency,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) MinuteRecords(ctx context.Context, serial string, from, to time.Time) ([]MinuteRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT serial, timestamp, import_j, export_j, gen_pos_j, gen_neg_j,
			   h1d_j, h2d_j, h3d_j, h1b_j, h2b_j, h3b_j, voltage, frequency
		FROM zappi_minute
		WHERE serial = ? AND timestamp >= ? AND timestamp < ?
		ORDER BY timestamp`,
		serial, from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []MinuteRecord
	for rows.Next() {
		var r MinuteRecord
		var ts string
		if err := rows.Scan(
			&r.Serial, &ts, &r.ImportJ, &r.ExportJ, &r.GenPosJ, &r.GenNegJ,
			&r.H1DJ, &r.H2DJ, &r.H3DJ, &r.H1BJ, &r.H2BJ, &r.H3BJ,
			&r.Voltage, &r.Frequency,
		); err != nil {
			return nil, err
		}
		r.Timestamp, _ = time.Parse(time.RFC3339, ts)
		records = append(records, r)
	}
	return records, rows.Err()
}

func (s *SQLiteStore) UpsertHourlyRecords(ctx context.Context, records []HourlyRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO zappi_hourly
			(serial, hour_start, import_kwh, export_kwh, generation_kwh, diverted_kwh, boosted_kwh)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, r := range records {
		_, err := stmt.ExecContext(ctx,
			r.Serial, r.HourStart.UTC().Format(time.RFC3339),
			r.ImportKWh, r.ExportKWh, r.GenerationKWh, r.DivertedKWh, r.BoostedKWh,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) HourlyRecords(ctx context.Context, serial string, from, to time.Time) ([]HourlyRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT serial, hour_start, import_kwh, export_kwh, generation_kwh, diverted_kwh, boosted_kwh
		FROM zappi_hourly
		WHERE serial = ? AND hour_start >= ? AND hour_start < ?
		ORDER BY hour_start`,
		serial, from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []HourlyRecord
	for rows.Next() {
		var r HourlyRecord
		var ts string
		if err := rows.Scan(
			&r.Serial, &ts, &r.ImportKWh, &r.ExportKWh,
			&r.GenerationKWh, &r.DivertedKWh, &r.BoostedKWh,
		); err != nil {
			return nil, err
		}
		r.HourStart, _ = time.Parse(time.RFC3339, ts)
		records = append(records, r)
	}
	return records, rows.Err()
}

func (s *SQLiteStore) UpsertDailyRecord(ctx context.Context, rec *DailyRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO zappi_daily
			(serial, date, import_kwh, export_kwh, generation_kwh, diverted_kwh, boosted_kwh,
			 self_consumption_pct, peak_generation_w, peak_import_w)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.Serial, rec.Date.Format("2006-01-02"),
		rec.ImportKWh, rec.ExportKWh, rec.GenerationKWh, rec.DivertedKWh, rec.BoostedKWh,
		rec.SelfConsumptionPct, rec.PeakGenerationW, rec.PeakImportW,
	)
	return err
}

func (s *SQLiteStore) DailyRecords(ctx context.Context, serial string, from, to time.Time) ([]DailyRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT serial, date, import_kwh, export_kwh, generation_kwh, diverted_kwh, boosted_kwh,
			   self_consumption_pct, peak_generation_w, peak_import_w
		FROM zappi_daily
		WHERE serial = ? AND date >= ? AND date < ?
		ORDER BY date`,
		serial, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []DailyRecord
	for rows.Next() {
		var r DailyRecord
		var dateStr string
		if err := rows.Scan(
			&r.Serial, &dateStr, &r.ImportKWh, &r.ExportKWh, &r.GenerationKWh,
			&r.DivertedKWh, &r.BoostedKWh, &r.SelfConsumptionPct,
			&r.PeakGenerationW, &r.PeakImportW,
		); err != nil {
			return nil, err
		}
		r.Date, _ = time.Parse("2006-01-02", dateStr)
		records = append(records, r)
	}
	return records, rows.Err()
}

// ── Period aggregation queries (weekly, monthly, quarterly, yearly) ──

// queryPeriodRecords runs a period aggregation query and scans the results.
func (s *SQLiteStore) queryPeriodRecords(ctx context.Context, query string, serial string, from, to time.Time) ([]PeriodRecord, error) {
	rows, err := s.db.QueryContext(ctx, query,
		serial, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []PeriodRecord
	for rows.Next() {
		var r PeriodRecord
		if err := rows.Scan(
			&r.Period, &r.ImportKWh, &r.ExportKWh,
			&r.GenerationKWh, &r.DivertedKWh, &r.BoostedKWh,
		); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

func (s *SQLiteStore) WeeklyRecords(ctx context.Context, serial string, from, to time.Time) ([]PeriodRecord, error) {
	return s.queryPeriodRecords(ctx, `
		SELECT strftime('%Y-W%W', date) AS period,
			   SUM(import_kwh), SUM(export_kwh), SUM(generation_kwh),
			   SUM(diverted_kwh), SUM(boosted_kwh)
		FROM zappi_daily
		WHERE serial = ? AND date >= ? AND date < ?
		GROUP BY period
		ORDER BY period`, serial, from, to)
}

func (s *SQLiteStore) MonthlyRecords(ctx context.Context, serial string, from, to time.Time) ([]PeriodRecord, error) {
	return s.queryPeriodRecords(ctx, `
		SELECT strftime('%Y-%m', date) AS period,
			   SUM(import_kwh), SUM(export_kwh), SUM(generation_kwh),
			   SUM(diverted_kwh), SUM(boosted_kwh)
		FROM zappi_daily
		WHERE serial = ? AND date >= ? AND date < ?
		GROUP BY period
		ORDER BY period`, serial, from, to)
}

func (s *SQLiteStore) QuarterlyRecords(ctx context.Context, serial string, from, to time.Time) ([]PeriodRecord, error) {
	return s.queryPeriodRecords(ctx, `
		SELECT strftime('%Y', date) || '-Q' || ((CAST(strftime('%m', date) AS INTEGER) - 1) / 3 + 1) AS period,
			   SUM(import_kwh), SUM(export_kwh), SUM(generation_kwh),
			   SUM(diverted_kwh), SUM(boosted_kwh)
		FROM zappi_daily
		WHERE serial = ? AND date >= ? AND date < ?
		GROUP BY period
		ORDER BY period`, serial, from, to)
}

func (s *SQLiteStore) YearlyRecords(ctx context.Context, serial string, from, to time.Time) ([]PeriodRecord, error) {
	return s.queryPeriodRecords(ctx, `
		SELECT strftime('%Y', date) AS period,
			   SUM(import_kwh), SUM(export_kwh), SUM(generation_kwh),
			   SUM(diverted_kwh), SUM(boosted_kwh)
		FROM zappi_daily
		WHERE serial = ? AND date >= ? AND date < ?
		GROUP BY period
		ORDER BY period`, serial, from, to)
}

func (s *SQLiteStore) GetBackfillState(ctx context.Context, serial string) (*BackfillState, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT serial, oldest_date, newest_date, status, last_error, updated_at
		FROM backfill_state WHERE serial = ?`, serial)

	state := &BackfillState{}
	var oldest, newest, updated string
	var lastErr sql.NullString
	err := row.Scan(&state.Serial, &oldest, &newest, &state.Status, &lastErr, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	state.OldestDate, _ = time.Parse("2006-01-02", oldest)
	state.NewestDate, _ = time.Parse("2006-01-02", newest)
	state.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	state.LastError = lastErr.String
	return state, nil
}

func (s *SQLiteStore) SaveBackfillState(ctx context.Context, state *BackfillState) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO backfill_state (serial, oldest_date, newest_date, status, last_error, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		state.Serial,
		state.OldestDate.Format("2006-01-02"),
		state.NewestDate.Format("2006-01-02"),
		state.Status, state.LastError,
		time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (s *SQLiteStore) GetExportState(ctx context.Context, exportType string) (*ExportState, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT export_type, last_date, updated_at
		FROM export_state WHERE export_type = ?`, exportType)

	state := &ExportState{}
	var lastDate, updated string
	err := row.Scan(&state.ExportType, &lastDate, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	state.LastDate, _ = time.Parse("2006-01-02", lastDate)
	state.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return state, nil
}

func (s *SQLiteStore) SaveExportState(ctx context.Context, state *ExportState) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO export_state (export_type, last_date, updated_at)
		VALUES (?, ?, ?)`,
		state.ExportType,
		state.LastDate.Format("2006-01-02"),
		time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (s *SQLiteStore) Serials(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT serial FROM backfill_state ORDER BY serial`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var serials []string
	for rows.Next() {
		var serial string
		if err := rows.Scan(&serial); err != nil {
			return nil, err
		}
		serials = append(serials, serial)
	}
	return serials, rows.Err()
}
