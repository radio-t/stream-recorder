package session

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testTTL = 30 * time.Minute

// fixedClock is a controllable time source.
type fixedClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fixedClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestController() (*Controller, *fixedClock) {
	clock := &fixedClock{now: time.Date(2026, 3, 28, 20, 0, 0, 0, time.UTC)}
	return NewController(testTTL, clock.Now), clock
}

func TestController_RequestAcceptedOnlyWhenIdle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T, c *Controller)
		want  bool // whether a manual request is accepted in this state
	}{
		{
			name:  "idle",
			setup: func(_ *testing.T, _ *Controller) {},
			want:  true,
		},
		{
			name: "already requested",
			setup: func(t *testing.T, c *Controller) {
				t.Helper()
				require.True(t, c.Request())
			},
			want: false,
		},
		{
			name: "session starting",
			setup: func(t *testing.T, c *Controller) {
				t.Helper()
				require.True(t, c.Begin(true))
			},
			want: false,
		},
		{
			name: "recording",
			setup: func(t *testing.T, c *Controller) {
				t.Helper()
				require.True(t, c.Begin(true))
				c.Started()
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, _ := newTestController()
			tt.setup(t, c)
			assert.Equal(t, !tt.want, c.Busy(), "Busy should be exactly when a request is refused")
			assert.Equal(t, tt.want, c.Request())
		})
	}
}

func TestController_RequestSurvivesFailureBeforeRecording(t *testing.T) {
	t.Parallel()

	c, _ := newTestController()
	require.True(t, c.Request())

	// a session starts and fails before any audio is written
	require.True(t, c.Begin(true))
	c.End()

	assert.True(t, c.Requested(), "a request must survive a session that never started recording")
	assert.Equal(t, Pending, c.State())
}

func TestController_RequestConsumedByRecording(t *testing.T) {
	t.Parallel()

	c, _ := newTestController()
	require.True(t, c.Request())

	require.True(t, c.Begin(true))
	c.Started()
	assert.True(t, c.Recording())
	c.End()

	assert.Equal(t, Idle, c.State(), "a request is spent once the recording actually ran")
	assert.False(t, c.Requested())
}

func TestController_RequestExpires(t *testing.T) {
	t.Parallel()

	c, clock := newTestController()
	require.True(t, c.Request())

	clock.advance(testTTL - time.Second)
	assert.True(t, c.Requested(), "a request should still be honoured before its deadline")

	clock.advance(2 * time.Second)
	assert.False(t, c.Requested(), "a request should be dropped once it expires")
	assert.Equal(t, Idle, c.State())
	assert.True(t, c.Request(), "an expired request leaves the recorder free to accept a new one")
}

func TestController_ExpiredRequestIsNotRetried(t *testing.T) {
	t.Parallel()

	c, clock := newTestController()
	require.True(t, c.Request())
	require.True(t, c.Begin(true))

	clock.advance(testTTL + time.Second)
	c.End() // the session failed, but the request is no longer live

	assert.Equal(t, Idle, c.State(), "an expired request should not be handed back for another attempt")
}

func TestController_BeginWithoutRequest(t *testing.T) {
	t.Parallel()

	c, _ := newTestController()
	require.True(t, c.Begin(true), "a scheduled recording starts without a manual request")
	c.Started()
	c.End()

	assert.Equal(t, Idle, c.State(), "a scheduled session leaves nothing pending")
}

func TestController_BeginRejectedWhileBusy(t *testing.T) {
	t.Parallel()

	c, _ := newTestController()
	require.True(t, c.Begin(true))
	assert.False(t, c.Begin(true), "a second session must not start while one is running")
}

func TestController_BeginOutsideScheduleNeedsLiveRequest(t *testing.T) {
	t.Parallel()

	t.Run("idle recorder does not start", func(t *testing.T) {
		t.Parallel()
		c, _ := newTestController()
		assert.False(t, c.Begin(false), "nothing may start outside the schedule without a request")
	})

	t.Run("pending request starts", func(t *testing.T) {
		t.Parallel()
		c, _ := newTestController()
		require.True(t, c.Request())
		assert.True(t, c.Begin(false))
	})

	t.Run("request expired while the stream was fetched", func(t *testing.T) {
		t.Parallel()
		c, clock := newTestController()
		require.True(t, c.Request())
		clock.advance(testTTL + time.Second)
		assert.False(t, c.Begin(false), "an expired request must not start a recording after all")
		assert.Equal(t, Idle, c.State())
	})
}

func TestController_StartedIgnoredWhenNotStarting(t *testing.T) {
	t.Parallel()

	c, _ := newTestController()
	c.Started()
	assert.Equal(t, Idle, c.State(), "a stray Started must not fake a recording")
}

func TestState_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "idle", Idle.String())
	assert.Equal(t, "pending", Pending.String())
	assert.Equal(t, "starting", Starting.String())
	assert.Equal(t, "recording", Recording.String())
	assert.Equal(t, "unknown", State(99).String())
}

func TestController_ConcurrentUse(t *testing.T) {
	t.Parallel()

	c := NewController(testTTL, nil)

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Request()
			c.Requested()
			c.Recording()
			if c.Begin(true) {
				c.Started()
				c.End()
			}
		}()
	}
	wg.Wait()

	assert.NotPanics(t, func() { _ = c.State() })
}
