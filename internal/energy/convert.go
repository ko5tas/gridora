package energy

// JoulesToKWh converts joules (watt-seconds) to kilowatt-hours.
func JoulesToKWh(joules float64) float64 {
	return joules / 3_600_000
}

// DeciVoltsToVolts converts deci-volts (API format) to volts.
func DeciVoltsToVolts(dv float64) float64 {
	return dv / 10
}
