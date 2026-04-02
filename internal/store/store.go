package store

import (
	"context"
	"time"
)

// StatusRecord represents a real-time Zappi status snapshot.
type StatusRecord struct {
	Serial          string
	Timestamp       time.Time
	GridW           float64
	GenerationW     float64
	DiversionW      float64
	Voltage         float64
	Frequency       float64
	ChargeAddedKWh  float64
	ZappiMode       int
	Status          int
	PlugStatus      string
	ECTP1W          float64
	ECTP2W          float64
	ECTP3W          float64
	ECTT1           string
	ECTT2           string
	ECTT3           string
}

// MinuteRecord represents one minute of historical energy data (raw joules).
type MinuteRecord struct {
	Serial    string
	Timestamp time.Time
	ImportJ   float64
	ExportJ   float64
	GenPosJ   float64
	GenNegJ   float64
	H1DJ      float64
	H2DJ      float64
	H3DJ      float64
	H1BJ      float64
	H2BJ      float64
	H3BJ      float64
	Voltage   float64
	Frequency float64
}

// HourlyRecord represents pre-aggregated hourly data (kWh).
type HourlyRecord struct {
	Serial        string
	HourStart     time.Time
	ImportKWh     float64
	ExportKWh     float64
	GenerationKWh float64
	DivertedKWh   float64
	BoostedKWh    float64
}

// DailyRecord represents pre-aggregated daily data (kWh).
type DailyRecord struct {
	Serial              string
	Date                time.Time
	ImportKWh           float64
	ExportKWh           float64
	GenerationKWh       float64
	DivertedKWh         float64
	BoostedKWh          float64
	SelfConsumptionPct  float64
	PeakGenerationW     float64
	PeakImportW         float64
}

// ExportState tracks the last successful export date per type.
type ExportState struct {
	ExportType string
	LastDate   time.Time
	UpdatedAt  time.Time
}

// BackfillState tracks how far back historical data has been fetched.
type BackfillState struct {
	Serial     string
	OldestDate time.Time
	NewestDate time.Time
	Status     string // "in_progress", "complete", "failed"
	LastError  string
	UpdatedAt  time.Time
}

// Store defines the data persistence interface.
type Store interface {
	// Schema
	Migrate(ctx context.Context) error
	Close() error

	// Status
	InsertStatus(ctx context.Context, rec *StatusRecord) error
	LatestStatus(ctx context.Context, serial string) (*StatusRecord, error)

	// Minute data
	UpsertMinuteRecords(ctx context.Context, records []MinuteRecord) error
	MinuteRecords(ctx context.Context, serial string, from, to time.Time) ([]MinuteRecord, error)

	// Aggregates
	UpsertHourlyRecords(ctx context.Context, records []HourlyRecord) error
	HourlyRecords(ctx context.Context, serial string, from, to time.Time) ([]HourlyRecord, error)
	UpsertDailyRecord(ctx context.Context, rec *DailyRecord) error
	DailyRecords(ctx context.Context, serial string, from, to time.Time) ([]DailyRecord, error)

	// Backfill
	GetBackfillState(ctx context.Context, serial string) (*BackfillState, error)
	SaveBackfillState(ctx context.Context, state *BackfillState) error

	// Export
	GetExportState(ctx context.Context, exportType string) (*ExportState, error)
	SaveExportState(ctx context.Context, state *ExportState) error
	Serials(ctx context.Context) ([]string, error)

	// Database path (for backup)
	DBPath() string
}
