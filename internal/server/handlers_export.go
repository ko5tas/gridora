package server

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ko5tas/gridora/internal/energy"
)

// handleExportDownload serves on-demand CSV or JSON export.
// Query params: serial, from, to, format (csv|json), resolution (minute|hourly|daily)
func (s *Server) handleExportDownload(w http.ResponseWriter, r *http.Request) {
	serial, from, to, ok := s.parseEnergyParams(w, r)
	if !ok {
		return
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "csv"
	}
	resolution := r.URL.Query().Get("resolution")
	if resolution == "" {
		resolution = "daily"
	}

	filename := fmt.Sprintf("gridora-%s-%s-to-%s",
		resolution,
		from.Format("2006-01-02"),
		to.AddDate(0, 0, -1).Format("2006-01-02"),
	)

	switch format {
	case "json":
		s.exportJSON(w, r, serial, from, to, resolution, filename)
	default:
		s.exportCSV(w, r, serial, from, to, resolution, filename)
	}
}

func (s *Server) exportCSV(w http.ResponseWriter, r *http.Request, serial string, from, to time.Time, resolution, filename string) {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.csv"`, filename))

	cw := csv.NewWriter(w)
	defer cw.Flush()

	switch resolution {
	case "minute":
		cw.Write([]string{"timestamp", "import_kwh", "export_kwh", "generation_kwh", "diverted_kwh", "voltage"})
		records, err := s.store.MinuteRecords(r.Context(), serial, from, to)
		if err != nil {
			return
		}
		for _, rec := range records {
			cw.Write([]string{
				rec.Timestamp.Format("2006-01-02T15:04:05Z"),
				fmt.Sprintf("%.6f", energy.JoulesToKWh(rec.ImportJ)),
				fmt.Sprintf("%.6f", energy.JoulesToKWh(rec.ExportJ)),
				fmt.Sprintf("%.6f", energy.JoulesToKWh(rec.GenPosJ)),
				fmt.Sprintf("%.6f", energy.JoulesToKWh(rec.H1DJ+rec.H2DJ+rec.H3DJ)),
				fmt.Sprintf("%.1f", rec.Voltage),
			})
		}

	case "hourly":
		cw.Write([]string{"hour_start", "import_kwh", "export_kwh", "generation_kwh", "diverted_kwh"})
		records, err := s.store.HourlyRecords(r.Context(), serial, from, to)
		if err != nil {
			return
		}
		for _, rec := range records {
			cw.Write([]string{
				rec.HourStart.Format("2006-01-02T15:04:05Z"),
				fmt.Sprintf("%.4f", rec.ImportKWh),
				fmt.Sprintf("%.4f", rec.ExportKWh),
				fmt.Sprintf("%.4f", rec.GenerationKWh),
				fmt.Sprintf("%.4f", rec.DivertedKWh),
			})
		}

	default: // daily
		cw.Write([]string{"date", "import_kwh", "export_kwh", "generation_kwh", "diverted_kwh", "self_consumption_pct", "peak_generation_w"})
		records, err := s.store.DailyRecords(r.Context(), serial, from, to)
		if err != nil {
			return
		}
		for _, rec := range records {
			cw.Write([]string{
				rec.Date.Format("2006-01-02"),
				fmt.Sprintf("%.3f", rec.ImportKWh),
				fmt.Sprintf("%.3f", rec.ExportKWh),
				fmt.Sprintf("%.3f", rec.GenerationKWh),
				fmt.Sprintf("%.3f", rec.DivertedKWh),
				fmt.Sprintf("%.1f", rec.SelfConsumptionPct),
				fmt.Sprintf("%.0f", rec.PeakGenerationW),
			})
		}
	}
}

func (s *Server) exportJSON(w http.ResponseWriter, r *http.Request, serial string, from, to time.Time, resolution, filename string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.json"`, filename))

	var data any
	var err error

	switch resolution {
	case "minute":
		data, err = s.store.MinuteRecords(r.Context(), serial, from, to)
	case "hourly":
		data, err = s.store.HourlyRecords(r.Context(), serial, from, to)
	default:
		data, err = s.store.DailyRecords(r.Context(), serial, from, to)
	}

	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(data)
}
