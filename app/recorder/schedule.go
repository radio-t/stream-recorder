package recorder

import "time"

// hoursInWeek is the number of hours in a week
const hoursInWeek = 7 * 24

// schedule constants for Radio-T show: Saturday 20:00 UTC, 2h before, 4h after
const (
	showDay         = time.Saturday
	showHour        = 20
	showBeforeHours = 2
	showAfterHours  = 4
)

// InScheduleWindow checks whether now falls within the recording window
// around the Radio-T show (Saturday 20:00 UTC, recording from 18:00 to 00:00 Sunday).
func InScheduleWindow(now time.Time) bool {
	schedHour := int(showDay)*24 + showHour
	startHour := (schedHour - showBeforeHours + hoursInWeek) % hoursInWeek
	endHour := (schedHour + showAfterHours) % hoursInWeek

	now = now.UTC()
	nowHour := int(now.Weekday())*24 + now.Hour()

	if startHour <= endHour {
		return nowHour >= startHour && nowHour < endHour
	}
	// window wraps around the week boundary (Saturday evening to Sunday morning)
	return nowHour >= startHour || nowHour < endHour
}
