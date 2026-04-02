package energy

import (
	"time"

	"github.com/ko5tas/gridora/internal/store"
)

// AggregateHourly groups minute records by hour and sums joules into kWh.
func AggregateHourly(serial string, minutes []store.MinuteRecord) []store.HourlyRecord {
	buckets := make(map[time.Time]*store.HourlyRecord)

	for _, m := range minutes {
		hourStart := m.Timestamp.Truncate(time.Hour)
		h, ok := buckets[hourStart]
		if !ok {
			h = &store.HourlyRecord{
				Serial:    serial,
				HourStart: hourStart,
			}
			buckets[hourStart] = h
		}
		h.ImportKWh += JoulesToKWh(m.ImportJ)
		h.ExportKWh += JoulesToKWh(m.ExportJ)
		h.GenerationKWh += JoulesToKWh(m.GenPosJ)
		h.DivertedKWh += JoulesToKWh(m.H1DJ + m.H2DJ + m.H3DJ)
		h.BoostedKWh += JoulesToKWh(m.H1BJ + m.H2BJ + m.H3BJ)
	}

	records := make([]store.HourlyRecord, 0, len(buckets))
	for _, h := range buckets {
		records = append(records, *h)
	}
	return records
}

// AggregateDaily computes a daily summary from minute records.
func AggregateDaily(serial string, date time.Time, minutes []store.MinuteRecord) *store.DailyRecord {
	d := &store.DailyRecord{
		Serial: serial,
		Date:   date.Truncate(24 * time.Hour),
	}

	for _, m := range minutes {
		d.ImportKWh += JoulesToKWh(m.ImportJ)
		d.ExportKWh += JoulesToKWh(m.ExportJ)
		d.GenerationKWh += JoulesToKWh(m.GenPosJ)
		d.DivertedKWh += JoulesToKWh(m.H1DJ + m.H2DJ + m.H3DJ)
		d.BoostedKWh += JoulesToKWh(m.H1BJ + m.H2BJ + m.H3BJ)

		// Track peak watts (joules per minute ≈ watts / 60, but raw joules/60 gives average watts)
		avgGenW := m.GenPosJ / 60
		avgImpW := m.ImportJ / 60
		if avgGenW > d.PeakGenerationW {
			d.PeakGenerationW = avgGenW
		}
		if avgImpW > d.PeakImportW {
			d.PeakImportW = avgImpW
		}
	}

	if d.GenerationKWh > 0 {
		selfUse := d.GenerationKWh - d.ExportKWh
		if selfUse < 0 {
			selfUse = 0
		}
		d.SelfConsumptionPct = (selfUse / d.GenerationKWh) * 100
	}

	return d
}
