package recorder

import (
	"fmt"
	"strings"
	"time"
)

// HoursInWeek is the number of hours in a week, used for schedule window validation.
const HoursInWeek = 7 * 24

// Schedule defines a weekly recording window around a show time.
type Schedule struct {
	Day         time.Weekday
	Hour        int // 0-23, UTC hour of the show start
	BeforeHours int // hours before show to start recording
	AfterHours  int // hours after show start to continue recording
}

// InWindow checks whether now falls within the recording window.
func (s Schedule) InWindow(now time.Time) bool {
	schedHour := int(s.Day)*24 + s.Hour
	startHour := (schedHour - s.BeforeHours + HoursInWeek) % HoursInWeek
	endHour := (schedHour + s.AfterHours) % HoursInWeek

	now = now.UTC()
	nowHour := int(now.Weekday())*24 + now.Hour()

	if startHour <= endHour {
		return nowHour >= startHour && nowHour < endHour
	}
	// window wraps around the week boundary (e.g. Saturday evening to Sunday morning)
	return nowHour >= startHour || nowHour < endHour
}

// ParseDay converts a day name string to time.Weekday.
func ParseDay(s string) (time.Weekday, error) {
	days := map[string]time.Weekday{
		"sunday":    time.Sunday,
		"monday":    time.Monday,
		"tuesday":   time.Tuesday,
		"wednesday": time.Wednesday,
		"thursday":  time.Thursday,
		"friday":    time.Friday,
		"saturday":  time.Saturday,
	}
	day, ok := days[strings.ToLower(strings.TrimSpace(s))]
	if !ok {
		return 0, fmt.Errorf("invalid day of week: %q", s)
	}
	return day, nil
}
