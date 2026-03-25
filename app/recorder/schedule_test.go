package recorder_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/radio-t/stream-recorder/app/recorder"
)

func TestSchedule_InWindow(t *testing.T) {
	t.Parallel()

	// saturday 20:00 UTC, 2h before, 4h after → Saturday 18:00 to Sunday 00:00 UTC
	saturdayShow := recorder.Schedule{Day: time.Saturday, Hour: 20, BeforeHours: 2, AfterHours: 4}

	tests := []struct {
		name     string
		schedule recorder.Schedule
		now      time.Time
		want     bool
	}{
		{
			name:     "inside window, during show",
			schedule: saturdayShow,
			now:      time.Date(2026, 3, 28, 21, 0, 0, 0, time.UTC), // Saturday 21:00
			want:     true,
		},
		{
			name:     "inside window, at show start",
			schedule: saturdayShow,
			now:      time.Date(2026, 3, 28, 20, 0, 0, 0, time.UTC), // Saturday 20:00
			want:     true,
		},
		{
			name:     "inside window, at window start boundary",
			schedule: saturdayShow,
			now:      time.Date(2026, 3, 28, 18, 0, 0, 0, time.UTC), // Saturday 18:00
			want:     true,
		},
		{
			name:     "inside window, mid-hour",
			schedule: saturdayShow,
			now:      time.Date(2026, 3, 28, 19, 30, 0, 0, time.UTC), // Saturday 19:30
			want:     true,
		},
		{
			name:     "outside window, hour before start",
			schedule: saturdayShow,
			now:      time.Date(2026, 3, 28, 17, 0, 0, 0, time.UTC), // Saturday 17:00
			want:     false,
		},
		{
			name:     "outside window, at end boundary (exclusive)",
			schedule: saturdayShow,
			now:      time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC), // Sunday 00:00
			want:     false,
		},
		{
			name:     "outside window, well after show",
			schedule: saturdayShow,
			now:      time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC), // Monday 10:00
			want:     false,
		},
		{
			name:     "outside window, same day before window",
			schedule: saturdayShow,
			now:      time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC), // Saturday 10:00
			want:     false,
		},

		// week wraparound: window extends past midnight into next day
		{
			name:     "wraparound, inside on next day",
			schedule: recorder.Schedule{Day: time.Saturday, Hour: 22, BeforeHours: 1, AfterHours: 4},
			now:      time.Date(2026, 3, 29, 1, 0, 0, 0, time.UTC), // Sunday 01:00
			want:     true,
		},
		{
			name:     "wraparound, at window start",
			schedule: recorder.Schedule{Day: time.Saturday, Hour: 22, BeforeHours: 1, AfterHours: 4},
			now:      time.Date(2026, 3, 28, 21, 0, 0, 0, time.UTC), // Saturday 21:00
			want:     true,
		},
		{
			name:     "wraparound, outside after end",
			schedule: recorder.Schedule{Day: time.Saturday, Hour: 22, BeforeHours: 1, AfterHours: 4},
			now:      time.Date(2026, 3, 29, 2, 0, 0, 0, time.UTC), // Sunday 02:00
			want:     false,
		},

		// before-hours pushes window start to previous day
		{
			name:     "before-hours crosses day boundary, inside previous day",
			schedule: recorder.Schedule{Day: time.Sunday, Hour: 1, BeforeHours: 3, AfterHours: 2},
			now:      time.Date(2026, 3, 28, 23, 0, 0, 0, time.UTC), // Saturday 23:00
			want:     true,
		},
		{
			name:     "before-hours crosses day boundary, inside show day",
			schedule: recorder.Schedule{Day: time.Sunday, Hour: 1, BeforeHours: 3, AfterHours: 2},
			now:      time.Date(2026, 3, 29, 2, 0, 0, 0, time.UTC), // Sunday 02:00
			want:     true,
		},
		{
			name:     "before-hours crosses day boundary, outside before start",
			schedule: recorder.Schedule{Day: time.Sunday, Hour: 1, BeforeHours: 3, AfterHours: 2},
			now:      time.Date(2026, 3, 28, 21, 0, 0, 0, time.UTC), // Saturday 21:00
			want:     false,
		},

		// different day of week
		{
			name:     "wednesday show, inside window",
			schedule: recorder.Schedule{Day: time.Wednesday, Hour: 19, BeforeHours: 1, AfterHours: 3},
			now:      time.Date(2026, 3, 25, 20, 0, 0, 0, time.UTC), // Wednesday 20:00
			want:     true,
		},
		{
			name:     "wednesday show, outside window",
			schedule: recorder.Schedule{Day: time.Wednesday, Hour: 19, BeforeHours: 1, AfterHours: 3},
			now:      time.Date(2026, 3, 25, 23, 0, 0, 0, time.UTC), // Wednesday 23:00
			want:     false,
		},

		// non-UTC timezone is converted to UTC
		{
			name:     "non-UTC timezone converted correctly",
			schedule: saturdayShow,
			now:      time.Date(2026, 3, 28, 23, 0, 0, 0, time.FixedZone("MSK", 3*60*60)), // 23:00 MSK = 20:00 UTC
			want:     true,
		},

		// zero before+after hours: zero-width window, never in window
		{
			name:     "zero before and after hours, never in window",
			schedule: recorder.Schedule{Day: time.Saturday, Hour: 20},
			now:      time.Date(2026, 3, 28, 20, 0, 0, 0, time.UTC),
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.schedule.InWindow(tt.now))
		})
	}
}

func TestParseDay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    time.Weekday
		wantErr bool
	}{
		{name: "saturday lowercase", input: "saturday", want: time.Saturday},
		{name: "Saturday mixed case", input: "Saturday", want: time.Saturday},
		{name: "SUNDAY uppercase", input: "SUNDAY", want: time.Sunday},
		{name: "monday", input: "monday", want: time.Monday},
		{name: "with whitespace", input: "  friday  ", want: time.Friday},
		{name: "invalid day", input: "notaday", wantErr: true},
		{name: "empty string", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := recorder.ParseDay(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
