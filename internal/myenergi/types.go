package myenergi

import "encoding/json"

// ZappiStatus represents a real-time Zappi status response.
// Field names match the myenergi API JSON keys.
type ZappiStatus struct {
	SerialNumber json.Number `json:"sno"` // Comes as a number from the API, use .String() for text
	Dat          string  `json:"dat"` // Date: DD-MM-YYYY
	Tim          string  `json:"tim"` // Time: HH:MM:SS
	Grid         float64 `json:"grd"` // Grid watts (negative = exporting)
	Generation   float64 `json:"gen"` // Generation watts
	Diversion    float64 `json:"div"` // Diverted to Zappi watts
	Voltage      float64 `json:"vol"` // Supply voltage (in deci-volts, /10 for volts)
	Frequency    float64 `json:"frq"` // Supply frequency Hz
	ChargeAdded  float64 `json:"che"` // Charge added this session kWh
	Mode         int     `json:"zmo"` // 1=Fast, 2=Eco, 3=Eco+, 4=Stopped
	Status       int     `json:"sta"` // 1=Paused, 3=Charging, 5=Complete
	PlugStatus   string  `json:"pst"` // A=EV disconnected, B1=EV connected, etc.
	ECTP1        float64 `json:"ectp1"` // CT1 watts
	ECTP2        float64 `json:"ectp2"` // CT2 watts
	ECTP3        float64 `json:"ectp3"` // CT3 watts
	ECTT1        string  `json:"ectt1"` // CT1 name (Internal/Grid/Generation)
	ECTT2        string  `json:"ectt2"` // CT2 name
	ECTT3        string  `json:"ectt3"` // CT3 name
}

// ZappiStatusResponse wraps the API response which returns an array of Zappi statuses.
type ZappiStatusResponse struct {
	Zappi []ZappiStatus `json:"zappi"`
}

// MinuteRecord represents one minute of historical energy data.
// Values are in joules (watt-seconds). Divide by 3,600,000 for kWh.
type MinuteRecord struct {
	Year      int     `json:"yr"`
	Month     int     `json:"mon"`
	Day       int     `json:"dom"` // Day of month
	Hour      int     `json:"hr"`
	Minute    int     `json:"min"`
	Import    float64 `json:"imp"` // Imported joules this minute
	Export    float64 `json:"exp"` // Exported joules this minute
	GenPos    float64 `json:"gep"` // Generation positive (actual PV output) joules
	GenNeg    float64 `json:"gen"` // Generation negative (inverter idle draw) joules
	H1D       float64 `json:"h1d"` // Phase 1 diverted joules
	H2D       float64 `json:"h2d"` // Phase 2 diverted joules
	H3D       float64 `json:"h3d"` // Phase 3 diverted joules
	H1B       float64 `json:"h1b"` // Phase 1 boost joules
	H2B       float64 `json:"h2b"` // Phase 2 boost joules
	H3B       float64 `json:"h3b"` // Phase 3 boost joules
	Voltage   float64 `json:"v1"`  // Voltage (in deci-volts)
	Frequency float64 `json:"frq"` // Frequency Hz
}

// MinuteDataResponse wraps the API response for per-minute data.
// The key is "U" + serial number.
type MinuteDataResponse map[string][]MinuteRecord

// HourRecord represents one hour of historical data.
type HourRecord struct {
	Year   int     `json:"yr"`
	Month  int     `json:"mon"`
	Day    int     `json:"dom"`
	Hour   int     `json:"hr"`
	Import float64 `json:"imp"`
	Export float64 `json:"exp"`
	GenPos float64 `json:"gep"`
	GenNeg float64 `json:"gen"`
	H1D    float64 `json:"h1d"`
	H2D    float64 `json:"h2d"`
	H3D    float64 `json:"h3d"`
	H1B    float64 `json:"h1b"`
	H2B    float64 `json:"h2b"`
	H3B    float64 `json:"h3b"`
}
