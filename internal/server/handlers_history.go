package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ko5tas/gridora/internal/energy"
)

// handleEnergyMinute returns minute-level data as JSON.
// Query params: serial, from (YYYY-MM-DD), to (YYYY-MM-DD)
func (s *Server) handleEnergyMinute(w http.ResponseWriter, r *http.Request) {
	serial, from, to, ok := s.parseEnergyParams(w, r)
	if !ok {
		return
	}

	records, err := s.store.MinuteRecords(r.Context(), serial, from, to)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	type minuteJSON struct {
		Timestamp     string  `json:"t"`
		ImportKWh     float64 `json:"import"`
		ExportKWh     float64 `json:"export"`
		GenerationKWh float64 `json:"generation"`
		DivertedKWh   float64 `json:"diverted"`
		BoostedKWh    float64 `json:"boosted"`
		Voltage       float64 `json:"voltage"`
	}

	result := make([]minuteJSON, 0, len(records))
	for _, r := range records {
		result = append(result, minuteJSON{
			Timestamp:     r.Timestamp.Format(time.RFC3339),
			ImportKWh:     energy.JoulesToKWh(r.ImportJ),
			ExportKWh:     energy.JoulesToKWh(r.ExportJ),
			GenerationKWh: energy.JoulesToKWh(r.GenPosJ),
			DivertedKWh:   energy.JoulesToKWh(r.H1DJ + r.H2DJ + r.H3DJ),
			BoostedKWh:    energy.JoulesToKWh(r.H1BJ + r.H2BJ + r.H3BJ),
			Voltage:       r.Voltage,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleEnergyHourly returns hourly-aggregated data as JSON.
func (s *Server) handleEnergyHourly(w http.ResponseWriter, r *http.Request) {
	serial, from, to, ok := s.parseEnergyParams(w, r)
	if !ok {
		return
	}

	records, err := s.store.HourlyRecords(r.Context(), serial, from, to)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	type hourlyJSON struct {
		Timestamp     string  `json:"t"`
		ImportKWh     float64 `json:"import"`
		ExportKWh     float64 `json:"export"`
		GenerationKWh float64 `json:"generation"`
		DivertedKWh   float64 `json:"diverted"`
		BoostedKWh    float64 `json:"boosted"`
	}

	result := make([]hourlyJSON, 0, len(records))
	for _, r := range records {
		result = append(result, hourlyJSON{
			Timestamp:     r.HourStart.Format(time.RFC3339),
			ImportKWh:     r.ImportKWh,
			ExportKWh:     r.ExportKWh,
			GenerationKWh: r.GenerationKWh,
			DivertedKWh:   r.DivertedKWh,
			BoostedKWh:    r.BoostedKWh,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleEnergyDaily returns daily-aggregated data as JSON.
func (s *Server) handleEnergyDaily(w http.ResponseWriter, r *http.Request) {
	serial, from, to, ok := s.parseEnergyParams(w, r)
	if !ok {
		return
	}

	records, err := s.store.DailyRecords(r.Context(), serial, from, to)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	type dailyJSON struct {
		Date               string  `json:"date"`
		ImportKWh          float64 `json:"import"`
		ExportKWh          float64 `json:"export"`
		GenerationKWh      float64 `json:"generation"`
		DivertedKWh        float64 `json:"diverted"`
		BoostedKWh         float64 `json:"boosted"`
		SelfConsumptionPct float64 `json:"self_consumption_pct"`
		PeakGenerationW    float64 `json:"peak_generation_w"`
	}

	result := make([]dailyJSON, 0, len(records))
	for _, r := range records {
		result = append(result, dailyJSON{
			Date:               r.Date.Format("2006-01-02"),
			ImportKWh:          r.ImportKWh,
			ExportKWh:          r.ExportKWh,
			GenerationKWh:      r.GenerationKWh,
			DivertedKWh:        r.DivertedKWh,
			BoostedKWh:         r.BoostedKWh,
			SelfConsumptionPct: r.SelfConsumptionPct,
			PeakGenerationW:    r.PeakGenerationW,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// parseEnergyParams extracts serial, from, to from query string.
// Defaults: serial=first known, from=7 days ago, to=today.
func (s *Server) parseEnergyParams(w http.ResponseWriter, r *http.Request) (string, time.Time, time.Time, bool) {
	serial := r.URL.Query().Get("serial")
	if serial == "" {
		serials, _ := s.store.Serials(r.Context())
		if len(serials) > 0 {
			serial = serials[0]
		} else {
			http.Error(w, "No devices found", http.StatusNotFound)
			return "", time.Time{}, time.Time{}, false
		}
	}

	now := time.Now().UTC().Truncate(24 * time.Hour)
	from := now.AddDate(0, 0, -7)
	to := now.AddDate(0, 0, 1) // Include today

	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			to = t.AddDate(0, 0, 1) // Make "to" inclusive
		}
	}

	return serial, from, to, true
}
