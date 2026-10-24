/*
Package sfu is the selective forwarding unit built into the server.

A mesh call has everybody send to everybody, which is free for the server and
past four or five people too much for anybody's upload. A forwarded call has
each person send once, to the server, and the server forward each stream to
everybody else — choosing, for a camera sent in simulcast, which of its copies
each receiver gets. Nothing is decoded or re-encoded: packets are copied.

Each participant holds one peer connection to the unit, carrying what they send
and everything they receive. Negotiation travels on the live connection as the
mesh's does, addressed to the unit rather than to a person; the unit is the
impolite side of perfect negotiation, so when both sides offer at once the
participant's browser yields.

What the unit can see, stated plainly because it differs from a mesh call:
DTLS-SRTP ends here, so the media is in the clear inside this process while it
is forwarded. It is never written anywhere, but an operator who can read the
process's memory could hear the call.
*/
package sfu

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

// Config is how one node's unit reaches the network. Per node, because each
// node has its own addresses.
type Config struct {
	/*
		UDPPorts is one port ("7882"), which every participant shares, told
		apart by their ICE credentials; or a range ("50000-50100"), one port
		per participant.
	*/
	UDPPorts string
	// PublicIPs are the addresses participants reach this node by, where it
	// sits behind a one-to-one NAT and cannot see them itself.
	PublicIPs []string
	// Interfaces limits which network interfaces are offered. Empty is all.
	Interfaces []string
	// Loopback offers the loopback interface too: for tests on one machine.
	Loopback bool
}

// Sender passes the unit's half of a negotiation to one participant.
type Sender func(callID, to uuid.UUID, signal json.RawMessage)

/*
Signal is one message of a forwarded call's negotiation, either way.

Description and Candidate are the WebRTC negotiation. Layers, from a
participant, says which copy of each other person's camera they want, by user
id: "f", "h", "q" or "off". Reset, from a participant, throws away their
connection to the unit so they can make a new one.
*/
type Signal struct {
	Description *webrtc.SessionDescription `json:"description,omitempty"`
	Candidate   *webrtc.ICECandidateInit   `json:"candidate,omitempty"`
	Layers      map[string]Layer           `json:"layers,omitempty"`
	Reset       bool                       `json:"reset,omitempty"`
}

// Unit forwards the calls this node holds.
type Unit struct {
	config Config
	send   Sender
	log    *slog.Logger

	bound    sync.Once
	bindErr  error
	settings webrtc.SettingEngine
	mux      io.Closer

	mu    sync.Mutex
	calls map[uuid.UUID]*call
	owned map[uuid.UUID]bool
}

// New makes a unit. It binds nothing until the first forwarded call needs it.
func New(config Config, send Sender, log *slog.Logger) *Unit {
	return &Unit{config: config, send: send, log: log, calls: map[uuid.UUID]*call{}, owned: map[uuid.UUID]bool{}}
}

// ParsePorts reads a port ("7882") or a range ("50000-50100").
func ParsePorts(value string) (uint16, uint16, error) {
	low, high, isRange := strings.Cut(strings.TrimSpace(value), "-")
	if !isRange {
		high = low
	}
	from, errFrom := strconv.ParseUint(strings.TrimSpace(low), 10, 16)
	to, errTo := strconv.ParseUint(strings.TrimSpace(high), 10, 16)
	// Zero is any free port, which only a test wants.
	if errFrom != nil || errTo != nil || to < from || (from == 0 && to != 0) {
		return 0, 0, fmt.Errorf("expected a UDP port or a range such as 50000-50100, not %q", value)
	}
	return uint16(from), uint16(to), nil
}

/*
Ready binds the unit's UDP port or range, once, and reports whether it could.
A unit that cannot bind forwards nothing, and calls stay meshes.
*/
func (u *Unit) Ready() error {
	u.bound.Do(func() { u.bindErr = u.bind() })
	return u.bindErr
}

func (u *Unit) bind() error {
	low, high, err := ParsePorts(u.config.UDPPorts)
	if err != nil {
		return err
	}
	var settings webrtc.SettingEngine
	settings.SetIncludeLoopbackCandidate(u.config.Loopback)
	settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6})
	keep := func(name string) bool {
		return len(u.config.Interfaces) == 0 || slices.Contains(u.config.Interfaces, name)
	}
	settings.SetInterfaceFilter(keep)
	if len(u.config.PublicIPs) > 0 {
		// Appended rather than replacing, so somebody on the server's own
		// network still reaches it without going out and back.
		if err := settings.SetICEAddressRewriteRules(webrtc.ICEAddressRewriteRule{
			External: u.config.PublicIPs, AsCandidateType: webrtc.ICECandidateTypeHost,
			Mode: webrtc.ICEAddressRewriteAppend,
		}); err != nil {
			return err
		}
	}
	if low == high {
		options := []ice.UDPMuxFromPortOption{ice.UDPMuxFromPortWithInterfaceFilter(keep)}
		if u.config.Loopback {
			options = append(options, ice.UDPMuxFromPortWithLoopback())
		}
		mux, err := ice.NewMultiUDPMuxFromPort(int(low), options...)
		if err != nil {
			return fmt.Errorf("listen on UDP port %d: %w", low, err)
		}
		settings.SetICEUDPMux(mux)
		u.mux = mux
	} else if err := settings.SetEphemeralUDPPortRange(low, high); err != nil {
		return err
	}
	u.settings = settings
	return nil
}

// Own records that this node forwards a call, so its negotiation is answered here.
func (u *Unit) Own(callID uuid.UUID) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.owned[callID] = true
}

// Holds says whether this node forwards a call.
func (u *Unit) Holds(callID uuid.UUID) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.owned[callID]
}

/*
Handle takes one message from a participant of a call this node forwards.

Who may send it is the caller's to have checked: that they are in the call and
allowed to be. The unit trusts the participant's identity and nothing else
they say.
*/
func (u *Unit) Handle(callID, from uuid.UUID, raw json.RawMessage) error {
	if err := u.Ready(); err != nil {
		return err
	}
	var signal Signal
	if err := json.Unmarshal(raw, &signal); err != nil {
		return fmt.Errorf("read signal: %w", err)
	}
	u.mu.Lock()
	if !u.owned[callID] {
		u.mu.Unlock()
		return errors.New("this node does not forward that call")
	}
	forwarded := u.calls[callID]
	if forwarded == nil {
		forwarded = &call{id: callID, unit: u, peers: map[uuid.UUID]*peer{}}
		u.calls[callID] = forwarded
	}
	u.mu.Unlock()
	return forwarded.handle(from, signal)
}

/*
Reconcile brings a forwarded call in line with who is in it: anybody not listed
is disconnected and stops receiving at once, not when their connection times
out. An empty list ends the call here.
*/
func (u *Unit) Reconcile(callID uuid.UUID, present []uuid.UUID) {
	u.mu.Lock()
	forwarded := u.calls[callID]
	if len(present) == 0 {
		delete(u.calls, callID)
		delete(u.owned, callID)
	}
	u.mu.Unlock()
	if forwarded != nil {
		forwarded.keepOnly(present)
	}
}

// Close disconnects everybody and releases the port.
func (u *Unit) Close() error {
	u.mu.Lock()
	calls := u.calls
	u.calls = map[uuid.UUID]*call{}
	u.owned = map[uuid.UUID]bool{}
	u.mu.Unlock()
	for _, each := range calls {
		each.keepOnly(nil)
	}
	if u.mux != nil {
		return u.mux.Close()
	}
	return nil
}
