package sfu

import (
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/rtp"
	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"
)

/*
A participant made of Pion, as a browser would be: one connection to the unit,
sending a microphone and a camera — the camera in simulcast if asked — and
receiving everybody else.
*/
type testClient struct {
	t    *testing.T
	user uuid.UUID
	pc   *webrtc.PeerConnection
	unit *Unit
	call uuid.UUID

	inbox chan Signal

	mu       sync.Mutex
	received map[string]*atomic.Int64 // packets by stream:track
	payloads map[string]byte          // the last layer marker seen, by stream
}

// The marker after the VP8 descriptor and key frame header says which copy a
// packet came from, so a receiver can tell which it was sent.
var markers = map[string]byte{"q": 'q', "h": 'h', "f": 'f', "": 's'}

func newTestClient(t *testing.T, unit *Unit, callID uuid.UUID, simulcast bool) *testClient {
	t.Helper()
	var settings webrtc.SettingEngine
	settings.SetIncludeLoopbackCandidate(true)
	settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	settings.SetInterfaceFilter(func(name string) bool { return strings.HasPrefix(name, "lo") })
	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(settings)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	c := &testClient{
		t: t, user: uuid.New(), pc: pc, unit: unit, call: callID, inbox: make(chan Signal, 256),
		received: map[string]*atomic.Int64{}, payloads: map[string]byte{},
	}
	t.Cleanup(func() { _ = pc.Close() })

	audio, _ := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", "me")
	if _, err := pc.AddTransceiverFromTrack(audio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly}); err != nil {
		t.Fatal(err)
	}
	vp8 := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000}
	videos := map[string]*webrtc.TrackLocalStaticRTP{}
	var simulcastSender *webrtc.RTPTransceiver
	if simulcast {
		for _, rid := range []string{"q", "h", "f"} {
			videos[rid], _ = webrtc.NewTrackLocalStaticRTP(vp8, "video", "me", webrtc.WithRTPStreamID(rid))
		}
		transceiver, err := pc.AddTransceiverFromTrack(videos["q"], webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
		if err != nil {
			t.Fatal(err)
		}
		simulcastSender = transceiver
		for _, rid := range []string{"h", "f"} {
			if err := transceiver.Sender().AddEncoding(videos[rid]); err != nil {
				t.Fatal(err)
			}
		}
	} else {
		videos[""], _ = webrtc.NewTrackLocalStaticRTP(vp8, "video", "me")
		if _, err := pc.AddTransceiverFromTrack(videos[""], webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly}); err != nil {
			t.Fatal(err)
		}
	}

	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate != nil {
			init := candidate.ToJSON()
			c.send(Signal{Candidate: &init})
		}
	})
	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		key := remote.StreamID() + "/" + remote.Kind().String()
		c.mu.Lock()
		count := c.received[key]
		if count == nil {
			count = &atomic.Int64{}
			c.received[key] = count
		}
		c.mu.Unlock()
		go func() {
			for {
				packet, _, err := remote.ReadRTP()
				if err != nil {
					return
				}
				count.Add(1)
				if remote.Kind() == webrtc.RTPCodecTypeVideo && len(packet.Payload) > 2 {
					c.mu.Lock()
					c.payloads[remote.StreamID()] = packet.Payload[2]
					c.mu.Unlock()
				}
			}
		}()
	})

	// Media from the start; nothing leaves before the connection is up.
	go func() {
		sequence := uint16(0)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			if pc.ConnectionState() == webrtc.PeerConnectionStateClosed {
				return
			}
			sequence++
			_ = audio.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: sequence, Timestamp: uint32(sequence) * 960}, Payload: []byte{0xfc, 0xff, 0xfe}})
			for rid, track := range videos {
				// A VP8 descriptor that starts a partition, then a key frame.
				payload := []byte{0x10, 0x00, markers[rid], 0x9d, 0x01, 0x2a}
				packet := &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: sequence, Timestamp: uint32(sequence) * 1800, Marker: true}, Payload: payload}
				if simulcastSender != nil {
					// A browser marks each copy with its rid and mid; Pion's
					// sender leaves that to whoever writes the packets.
					for _, extension := range simulcastSender.Sender().GetParameters().HeaderExtensions {
						switch extension.URI {
						case sdp.SDESMidURI:
							_ = packet.SetExtension(uint8(extension.ID), []byte(simulcastSender.Mid()))
						case sdp.SDESRTPStreamIDURI:
							_ = packet.SetExtension(uint8(extension.ID), []byte(rid))
						}
					}
				}
				_ = track.WriteRTP(packet)
			}
		}
	}()
	go c.negotiateForever()
	return c
}

func (c *testClient) send(signal Signal) {
	raw, _ := json.Marshal(signal)
	// Refusals are expected once somebody has been taken out, or the test is
	// over; what each test checks is what arrives.
	_ = c.unit.Handle(c.call, c.user, raw)
}

// negotiateForever answers the unit's offers and takes its candidates. The
// client offers once, first; after that only the unit offers, so there is no
// glare to settle.
func (c *testClient) negotiateForever() {
	for signal := range c.inbox {
		switch {
		case signal.Description != nil && signal.Description.Type == webrtc.SDPTypeOffer:
			if err := c.pc.SetRemoteDescription(*signal.Description); err != nil {
				continue // closed at the end of the test
			}
			answer, _ := c.pc.CreateAnswer(nil)
			_ = c.pc.SetLocalDescription(answer)
			c.send(Signal{Description: c.pc.LocalDescription()})
		case signal.Description != nil:
			_ = c.pc.SetRemoteDescription(*signal.Description)
		case signal.Candidate != nil:
			_ = c.pc.AddICECandidate(*signal.Candidate)
		}
	}
}

func (c *testClient) offer() {
	offer, err := c.pc.CreateOffer(nil)
	if err != nil {
		c.t.Fatal(err)
	}
	if err := c.pc.SetLocalDescription(offer); err != nil {
		c.t.Fatal(err)
	}
	c.send(Signal{Description: c.pc.LocalDescription()})
}

func (c *testClient) count(key string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if count := c.received[key]; count != nil {
		return count.Load()
	}
	return 0
}

// receiving counts the other people whose stream of a kind has had packets.
func (c *testClient) receiving(kind string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	people := 0
	for key, count := range c.received {
		if strings.HasSuffix(key, "/"+kind) && count.Load() > 5 {
			people++
		}
	}
	return people
}

func testUnit(t *testing.T, clients map[uuid.UUID]*testClient, mu *sync.Mutex) *Unit {
	t.Helper()
	unit := New(Config{UDPPorts: "0", Loopback: true, Interfaces: nil}, func(_, to uuid.UUID, raw json.RawMessage) {
		var signal Signal
		if err := json.Unmarshal(raw, &signal); err != nil {
			t.Errorf("the unit sent something unreadable: %v", err)
			return
		}
		mu.Lock()
		client := clients[to]
		mu.Unlock()
		if client != nil {
			client.inbox <- signal
		}
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { _ = unit.Close() })
	return unit
}

func eventually(t *testing.T, within time.Duration, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("not within %s: %s", within, what)
}

/*
Six people in one forwarded call each receive the other five, voice and camera.

Each sends once, to the unit. That is the whole point of it: in a mesh each of
the six would be sending five copies.
*/
func TestSixPeopleHearAndSeeEachOther(t *testing.T) {
	clients := map[uuid.UUID]*testClient{}
	var mu sync.Mutex
	unit := testUnit(t, clients, &mu)
	callID := uuid.New()
	unit.Own(callID)

	var people []*testClient
	for range 6 {
		client := newTestClient(t, unit, callID, false)
		mu.Lock()
		clients[client.user] = client
		mu.Unlock()
		people = append(people, client)
		client.offer()
	}
	for index, person := range people {
		eventually(t, 20*time.Second, "everybody receives the other five", func() bool {
			return person.receiving("video") == 5 && person.receiving("audio") == 5
		})
		if person.count(person.user.String()+":camera/video") > 0 {
			t.Errorf("person %d is sent their own camera back", index)
		}
	}

	// Somebody leaving stops being forwarded to, and from.
	unit.Reconcile(callID, []uuid.UUID{people[0].user, people[1].user, people[2].user, people[3].user, people[4].user})
	eventually(t, 10*time.Second, "the one who left is disconnected", func() bool {
		state := people[5].pc.ConnectionState()
		return state == webrtc.PeerConnectionStateClosed || state == webrtc.PeerConnectionStateFailed ||
			state == webrtc.PeerConnectionStateDisconnected
	})
}

/*
A receiver gets the copy of a camera they ask for, and a small tile is sent the
small copy: the unit forwards one of the three, not all of them.
*/
func TestEachReceiverGetsTheCopyItAskedFor(t *testing.T) {
	clients := map[uuid.UUID]*testClient{}
	var mu sync.Mutex
	unit := testUnit(t, clients, &mu)
	callID := uuid.New()
	unit.Own(callID)

	sender := newTestClient(t, unit, callID, true)
	small := newTestClient(t, unit, callID, false)
	large := newTestClient(t, unit, callID, false)
	for _, each := range []*testClient{sender, small, large} {
		mu.Lock()
		clients[each.user] = each
		mu.Unlock()
		each.offer()
	}
	stream := sender.user.String() + ":camera"
	marker := func(c *testClient) byte {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.payloads[stream]
	}
	eventually(t, 20*time.Second, "both receivers have the sender's camera", func() bool {
		return marker(small) != 0 && marker(large) != 0
	})

	small.send(Signal{Layers: map[string]Layer{sender.user.String(): LayerQuarter}})
	large.send(Signal{Layers: map[string]Layer{sender.user.String(): LayerFull}})
	eventually(t, 10*time.Second, "the small tile gets the quarter copy", func() bool { return marker(small) == 'q' })
	// The full copy needs the bandwidth estimate to allow it; on loopback the
	// controller starts at a megabit and climbs, so give it a while.
	eventually(t, 20*time.Second, "the large tile gets more than the quarter copy", func() bool {
		return marker(large) == 'f' || marker(large) == 'h'
	})

	large.send(Signal{Layers: map[string]Layer{sender.user.String(): LayerOff}})
	time.Sleep(300 * time.Millisecond)
	before := large.count(stream + "/video")
	time.Sleep(time.Second)
	if after := large.count(stream + "/video"); after-before > 3 {
		t.Errorf("a camera asked to be off was still sent: %d packets in a second", after-before)
	}
}

func TestLayersFitTheBudget(t *testing.T) {
	all := []Layer{LayerQuarter, LayerHalf, LayerFull}
	for _, example := range []struct {
		name    string
		budget  int
		voices  int
		choices []choice
		want    []Layer
	}{
		{"plenty", 10_000_000, 2, []choice{{LayerFull, all}, {LayerQuarter, all}}, []Layer{LayerFull, LayerQuarter}},
		{"no wish is a half", 10_000_000, 0, []choice{{"", all}}, []Layer{LayerHalf}},
		{"the largest gives way first", 2_000_000, 0, []choice{{LayerFull, all}, {LayerHalf, all}, {LayerHalf, all}}, []Layer{LayerHalf, LayerHalf, LayerHalf}},
		{"never below the smallest", 100_000, 4, []choice{{LayerFull, all}, {LayerFull, all}}, []Layer{LayerQuarter, LayerQuarter}},
		{"only what is sent", 10_000_000, 0, []choice{{LayerFull, []Layer{LayerQuarter, LayerHalf}}}, []Layer{LayerHalf}},
		{"off stays off", 10_000_000, 0, []choice{{LayerOff, all}}, []Layer{LayerOff}},
	} {
		got := pickLayers(example.budget, example.voices, example.choices)
		for index := range got {
			if got[index] != example.want[index] {
				t.Errorf("%s: got %v, want %v", example.name, got, example.want)
				break
			}
		}
	}
}

func TestKeyframesAreRecognised(t *testing.T) {
	for _, example := range []struct {
		name    string
		payload []byte
		want    bool
	}{
		{"a key frame", []byte{0x10, 0x00, 0x9d}, true},
		{"a delta frame", []byte{0x10, 0x01}, false},
		{"not the start of a frame", []byte{0x00, 0x00}, false},
		{"with a long picture id", []byte{0x90, 0x80, 0x81, 0x02, 0x00}, true},
		{"with a short picture id and TL0PICIDX", []byte{0x90, 0xc0, 0x01, 0x05, 0x01}, false},
		{"empty", nil, false},
	} {
		if got := vp8Keyframe(example.payload); got != example.want {
			t.Errorf("%s: %v, want %v", example.name, got, example.want)
		}
	}
}

func TestPortsAreReadAsOneOrARange(t *testing.T) {
	if low, high, err := ParsePorts("7882"); err != nil || low != 7882 || high != 7882 {
		t.Errorf("one port: %d %d %v", low, high, err)
	}
	if low, high, err := ParsePorts("50000-50100"); err != nil || low != 50000 || high != 50100 {
		t.Errorf("a range: %d %d %v", low, high, err)
	}
	for _, bad := range []string{"", "x", "50100-50000", "70000"} {
		if _, _, err := ParsePorts(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}
