// Package session tracks the lifecycle of a recording session and of manual requests to start one.
package session

import (
	"sync"
	"time"
)

// State is the lifecycle state of the recorder.
type State int

// Recorder lifecycle states.
const (
	Idle      State = iota // nothing recording, nothing requested
	Pending                // a manual recording was accepted and has not started yet
	Starting               // a session has begun, no audio written yet
	Recording              // audio is being written
)

// String returns the state name, for logging.
func (s State) String() string {
	switch s {
	case Idle:
		return "idle"
	case Pending:
		return "pending"
	case Starting:
		return "starting"
	case Recording:
		return "recording"
	default:
		return "unknown"
	}
}

// Controller is the recording lifecycle shared by the HTTP server and the recording loop.
// it replaces a pair of booleans that could both lose an accepted request and keep one long
// after it was made. safe for concurrent use.
type Controller struct {
	mu        sync.Mutex
	state     State
	expiresAt time.Time // when a pending request stops being honoured
	claimed   bool      // the current session started from a manual request
	ttl       time.Duration
	nowFn     func() time.Time
}

// NewController creates a controller whose accepted manual requests expire after ttl.
// nowFn may be nil, in which case time.Now is used.
func NewController(ttl time.Duration, nowFn func() time.Time) *Controller {
	if nowFn == nil {
		nowFn = time.Now
	}
	return &Controller{ttl: ttl, nowFn: nowFn} //nolint:exhaustruct // mu, state, expiresAt and claimed start at zero
}

// Request accepts a manual recording request and reports whether it was accepted.
// only an idle recorder accepts one, so a request made during a session cannot sit around and
// trigger an unintended recording later, and a second request cannot displace a waiting one.
func (c *Controller) Request() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expire()

	if c.state != Idle {
		return false
	}
	c.state = Pending
	c.expiresAt = c.nowFn().Add(c.ttl)
	return true
}

// Requested reports whether a manual recording is waiting to start.
func (c *Controller) Requested() bool {
	return c.State() == Pending
}

// Recording reports whether audio is being written.
func (c *Controller) Recording() bool {
	return c.State() == Recording
}

// Busy reports whether the recorder is anything other than idle, which is exactly when a manual
// request would be refused. a session that has begun but not yet written audio counts as busy.
func (c *Controller) Busy() bool {
	return c.State() != Idle
}

// Begin takes the recorder for a session and reports whether it was free to start.
// scheduled says the recorder may record on the schedule alone; when it is false the session
// is only allowed by a manual request, which is claimed here under the same lock, so a request
// that expired while the stream was being fetched cannot start a recording after all.
// claiming here also stops one request from starting two sessions.
func (c *Controller) Begin(scheduled bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expire()

	switch {
	case c.state == Pending:
		c.claimed = true
	case c.state == Idle && scheduled:
		c.claimed = false
	default:
		return false
	}
	c.state = Starting
	return true
}

// Started marks the point where the output file exists and audio begins to flow.
func (c *Controller) Started() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == Starting {
		c.state = Recording
	}
}

// End releases the recorder. a claimed session that never reached the point of writing audio
// hands its request back, so a failure before the recording starts is retried on a later poll,
// but only for as long as the request has left to live.
func (c *Controller) End() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == Starting && c.claimed && c.nowFn().Before(c.expiresAt) {
		c.state, c.claimed = Pending, false
		return
	}
	c.state, c.claimed = Idle, false
}

// State returns the current lifecycle state.
func (c *Controller) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expire()
	return c.state
}

// expire drops a manual request that has waited too long, so a button press cannot start a
// recording hours later when an unrelated stream appears. must be called with the lock held.
func (c *Controller) expire() {
	if c.state == Pending && !c.nowFn().Before(c.expiresAt) {
		c.state = Idle
	}
}
