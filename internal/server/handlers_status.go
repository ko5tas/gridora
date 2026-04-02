package server

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"time"
)

// handleDashboard renders the live dashboard page.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	serials, err := s.store.Serials(r.Context())
	if err != nil {
		s.logger.Error("failed to get serials", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	serial := ""
	if len(serials) > 0 {
		serial = serials[0]
	}

	// Serialize milestones as safe JS for the template's <script> block
	milestonesJSON := "[]"
	if len(s.config.Milestones) > 0 {
		if b, err := json.Marshal(s.config.Milestones); err == nil {
			milestonesJSON = string(b)
		}
	}

	data := map[string]any{
		"Serial":     serial,
		"Milestones": template.JS(milestonesJSON),
	}

	s.templates.ExecuteTemplate(w, "dashboard.html", data)
}

// handleAPIStatus returns the latest status as JSON.
func (s *Server) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	serials, err := s.store.Serials(r.Context())
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	type statusJSON struct {
		Serial         string  `json:"serial"`
		Timestamp      string  `json:"timestamp"`
		GridW          float64 `json:"grid_w"`
		GenerationW    float64 `json:"generation_w"`
		DiversionW     float64 `json:"diversion_w"`
		Voltage        float64 `json:"voltage"`
		Frequency      float64 `json:"frequency"`
		ChargeAddedKWh float64 `json:"charge_added_kwh"`
		ZappiMode      int     `json:"zappi_mode"`
		Status         int     `json:"status"`
	}

	var results []statusJSON
	for _, serial := range serials {
		rec, err := s.store.LatestStatus(r.Context(), serial)
		if err != nil || rec == nil {
			continue
		}
		results = append(results, statusJSON{
			Serial:         rec.Serial,
			Timestamp:      rec.Timestamp.Format(time.RFC3339),
			GridW:          rec.GridW,
			GenerationW:    rec.GenerationW,
			DiversionW:     rec.DiversionW,
			Voltage:        rec.Voltage,
			Frequency:      rec.Frequency,
			ChargeAddedKWh: rec.ChargeAddedKWh,
			ZappiMode:      rec.ZappiMode,
			Status:         rec.Status,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// handleSSE streams real-time status updates via Server-Sent Events.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	serial := r.URL.Query().Get("serial")
	if serial == "" {
		serials, _ := s.store.Serials(r.Context())
		if len(serials) > 0 {
			serial = serials[0]
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	// Send initial status immediately
	s.sendStatusEvent(w, flusher, r, serial)

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			s.sendStatusEvent(w, flusher, r, serial)
		}
	}
}

func (s *Server) sendStatusEvent(w http.ResponseWriter, flusher http.Flusher, r *http.Request, serial string) {
	rec, err := s.store.LatestStatus(r.Context(), serial)
	if err != nil || rec == nil {
		return
	}

	data := map[string]any{
		"serial":           rec.Serial,
		"timestamp":        rec.Timestamp.Format("02/01/2006 15:04:05"),
		"grid_w":           rec.GridW,
		"generation_w":     rec.GenerationW,
		"diversion_w":      rec.DiversionW,
		"voltage":          rec.Voltage,
		"frequency":        rec.Frequency,
		"charge_added_kwh": rec.ChargeAddedKWh,
		"zappi_mode":       rec.ZappiMode,
		"zappi_mode_name":  zappiModeName(rec.ZappiMode),
		"status":           rec.Status,
	}

	jsonBytes, _ := json.Marshal(data)
	fmt.Fprintf(w, "data: %s\n\n", jsonBytes)
	flusher.Flush()
}

func zappiModeName(mode int) string {
	switch mode {
	case 1:
		return "Fast"
	case 2:
		return "Eco"
	case 3:
		return "Eco+"
	case 4:
		return "Stopped"
	default:
		return "Unknown"
	}
}
