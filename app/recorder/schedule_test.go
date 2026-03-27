package recorder_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/radio-t/stream-recorder/app/recorder"
)

func TestInScheduleWindow(t *testing.T) {
	t.Parallel()

	// hardcoded: Saturday 20:00 UTC, 2h before (18:00), 4h after (00:00 Sunday)
	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{
			name: "inside window, during show",
			now:  time.Date(2026, 3, 28, 21, 0, 0, 0, time.UTC), // saturday 21:00
			want: true,
		},
		{
			name: "inside window, at show start",
			now:  time.Date(2026, 3, 28, 20, 0, 0, 0, time.UTC), // saturday 20:00
			want: true,
		},
		{
			name: "inside window, at window start boundary",
			now:  time.Date(2026, 3, 28, 18, 0, 0, 0, time.UTC), // saturday 18:00
			want: true,
		},
		{
			name: "inside window, mid-hour",
			now:  time.Date(2026, 3, 28, 19, 30, 0, 0, time.UTC), // saturday 19:30
			want: true,
		},
		{
			name: "inside window, just before end",
			now:  time.Date(2026, 3, 28, 23, 59, 0, 0, time.UTC), // saturday 23:59
			want: true,
		},
		{
			name: "outside window, hour before start",
			now:  time.Date(2026, 3, 28, 17, 0, 0, 0, time.UTC), // saturday 17:00
			want: false,
		},
		{
			name: "outside window, at end boundary (exclusive)",
			now:  time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC), // sunday 00:00
			want: false,
		},
		{
			name: "outside window, Monday",
			now:  time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC), // monday 10:00
			want: false,
		},
		{
			name: "outside window, Saturday morning",
			now:  time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC), // saturday 10:00
			want: false,
		},
		{
			name: "outside window, Sunday afternoon",
			now:  time.Date(2026, 3, 29, 14, 0, 0, 0, time.UTC), // sunday 14:00
			want: false,
		},
		{
			name: "non-UTC timezone converted correctly (MSK inside window)",
			now:  time.Date(2026, 3, 28, 23, 0, 0, 0, time.FixedZone("MSK", 3*60*60)), // 23:00 MSK = 20:00 UTC
			want: true,
		},
		{
			name: "non-UTC timezone converted correctly (MSK outside window)",
			now:  time.Date(2026, 3, 28, 20, 0, 0, 0, time.FixedZone("MSK", 3*60*60)), // 20:00 MSK = 17:00 UTC
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, recorder.InScheduleWindow(tt.now))
		})
	}
}
