package myenergi

import (
	"fmt"
	"time"
)

const (
	DirectorURL = "https://director.myenergi.net"
)

// StatusURL returns the endpoint for fetching all Zappi statuses.
func StatusURL(baseURL string) string {
	return baseURL + "/cgi-jstatus-Z"
}

// DayMinuteURL returns the endpoint for per-minute data for a specific date.
func DayMinuteURL(baseURL, serial string, date time.Time) string {
	return fmt.Sprintf("%s/cgi-jday-Z%s-%d-%d-%d",
		baseURL, serial,
		date.Year(), date.Month(), date.Day())
}

// DayHourURL returns the endpoint for hourly data for a specific date.
func DayHourURL(baseURL, serial string, date time.Time) string {
	return fmt.Sprintf("%s/cgi-jdayhour-Z%s-%d-%d-%d",
		baseURL, serial,
		date.Year(), date.Month(), date.Day())
}
