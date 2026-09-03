package path

import (
	"net/netip"
	"time"
)

// State of a path to one peer.
type State string

// Path states. Relay is reserved for spec 005.
const (
	Idle        State = "idle"
	Probing     State = "probing"
	Direct      State = "direct"
	Relay       State = "relay"
	Unreachable State = "unreachable"
)

// Action the daemon must perform after a Step.
type Action int

// Actions.
const (
	// ActNone: nothing to do.
	ActNone Action = iota
	// ActSink: point the peer at its local sink so traffic intent is
	// observable without sending anything.
	ActSink
	// ActProbe: re-add the peer with Output.Endpoint and send a trigger
	// packet so WireGuard initiates a handshake there.
	ActProbe
)

// Options tune the machine; zero values select the spec defaults.
type Options struct {
	// ProbeWindow is how long one candidate gets (2 s).
	ProbeWindow time.Duration
	// StallAfter is how long a direct path may go without a handshake
	// while traffic is queued before re-probing (3 min).
	StallAfter time.Duration
	// RetryAfter is the minimum gap between probe rounds toward an
	// unreachable peer (60 s).
	RetryAfter time.Duration
}

func (o Options) withDefaults() Options {
	if o.ProbeWindow <= 0 {
		o.ProbeWindow = 2 * time.Second
	}
	if o.StallAfter <= 0 {
		o.StallAfter = 3 * time.Minute
	}
	if o.RetryAfter <= 0 {
		o.RetryAfter = 60 * time.Second
	}
	return o
}

// Input is what the daemon observed since the last step.
type Input struct {
	Now time.Time
	// Intent is true when traffic toward the peer was seen (a packet at
	// its sink or an explicit ping).
	Intent bool
	// Handshake, Endpoint, Rx and Tx come from the device's stats.
	Handshake time.Time
	Endpoint  netip.AddrPort
	Rx, Tx    uint64
}

// Output is the decision of one step.
type Output struct {
	State State
	// Endpoint is the address in use (direct) or to try (probe); zero
	// while the peer points at its sink.
	Endpoint netip.AddrPort
	Action   Action
	// Changed is true when state or endpoint differ from the last step,
	// which is when the daemon reports the path.
	Changed bool
}

// Machine tracks one peer.
type Machine struct {
	opts       Options
	state      State
	candidates []netip.AddrPort
	dirty      bool
	idx        int
	endpoint   netip.AddrPort
	started    bool

	wanted      bool // intent was ever expressed
	freshIntent bool // intent since the last probe round started
	windowStart time.Time
	lastProbe   time.Time
	lastRound   time.Time
	handshake   time.Time
	rx, tx      uint64
	probes      int
}

// New returns a machine in Idle.
func New(opts Options) *Machine {
	return &Machine{opts: opts.withDefaults(), state: Idle}
}

// State reports the current state.
func (m *Machine) State() State { return m.state }

// Endpoint reports the endpoint in use or being tried.
func (m *Machine) Endpoint() netip.AddrPort { return m.endpoint }

// Probes counts candidates tried so far (tests and logs).
func (m *Machine) Probes() int { return m.probes }

// SetCandidates replaces the ordered candidate list. A change while
// probing restarts the round; a change while unreachable re-probes
// once traffic intent exists.
func (m *Machine) SetCandidates(list []netip.AddrPort) {
	if equal(m.candidates, list) {
		return
	}
	m.candidates = append([]netip.AddrPort(nil), list...)
	m.dirty = true
}

// Step advances the machine.
func (m *Machine) Step(in Input) Output {
	prevState, prevEndpoint := m.state, m.endpoint
	out := Output{Action: ActNone}
	if !m.started {
		m.started = true
		out.Action = ActSink
	}
	if in.Intent {
		m.wanted, m.freshIntent = true, true
	}
	advanced := in.Handshake.After(m.handshake)
	if advanced {
		m.handshake = in.Handshake
	}

	switch m.state {
	case Idle, Unreachable:
		switch {
		case advanced && in.Endpoint.IsValid():
			// The peer reached us first; WireGuard learned its address.
			m.state, m.endpoint = Direct, in.Endpoint
		case m.wanted && (m.state == Idle || m.dirty || (m.freshIntent && in.Now.Sub(m.lastRound) >= m.opts.RetryAfter)):
			out.Action = m.startRound(in.Now)
		}
	case Probing:
		switch {
		case advanced:
			m.state = Direct
			if in.Endpoint.IsValid() {
				m.endpoint = in.Endpoint
			}
		case m.dirty && in.Now.Sub(m.lastProbe) >= m.opts.ProbeWindow:
			out.Action = m.startRound(in.Now)
		case in.Now.Sub(m.windowStart) >= m.opts.ProbeWindow:
			m.idx++
			out.Action = m.probeNext(in.Now)
		}
	case Direct, Relay:
		if advanced && in.Endpoint.IsValid() {
			m.endpoint = in.Endpoint // roaming
		}
		stalled := !m.handshake.IsZero() && in.Now.Sub(m.handshake) > m.opts.StallAfter && in.Tx > m.tx && in.Rx == m.rx
		if stalled && m.wanted {
			out.Action = m.startRound(in.Now)
		}
	}
	m.rx, m.tx = in.Rx, in.Tx
	out.State, out.Endpoint = m.state, m.endpoint
	out.Changed = m.state != prevState || m.endpoint != prevEndpoint
	return out
}

// startRound begins probing from the first candidate.
func (m *Machine) startRound(now time.Time) Action {
	m.dirty, m.freshIntent = false, false
	m.lastRound = now
	m.idx = 0
	return m.probeNext(now)
}

// probeNext tries candidate idx or gives up.
func (m *Machine) probeNext(now time.Time) Action {
	if m.idx >= len(m.candidates) {
		m.state, m.endpoint = Unreachable, netip.AddrPort{}
		return ActSink
	}
	m.state = Probing
	m.endpoint = m.candidates[m.idx]
	m.windowStart, m.lastProbe = now, now
	m.probes++
	return ActProbe
}

func equal(a, b []netip.AddrPort) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
