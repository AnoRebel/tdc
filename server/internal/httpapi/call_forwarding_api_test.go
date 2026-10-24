package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// The forwarding unit, as a client addresses it: nobody.
const forwardingUnit = "00000000-0000-0000-0000-000000000000"

/*
forwardedClient is somebody in a forwarded call, made of Pion as a browser
would be: one connection to the unit, negotiated over their live connection,
sending a microphone and a camera and receiving everybody else's.
*/
type forwardedClient struct {
	pc     *webrtc.PeerConnection
	socket *websocket.Conn
	callID string

	writing  sync.Mutex
	mu       sync.Mutex
	received map[string]*atomic.Int64
}

func newForwardedClient(t *testing.T, ctx context.Context, socket *websocket.Conn, callID string) *forwardedClient {
	t.Helper()
	var engine webrtc.SettingEngine
	engine.SetIncludeLoopbackCandidate(true)
	engine.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	engine.SetInterfaceFilter(func(name string) bool { return strings.HasPrefix(name, "lo") })
	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(engine)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	c := &forwardedClient{pc: pc, socket: socket, callID: callID, received: map[string]*atomic.Int64{}}

	audio, _ := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", "me")
	video, _ := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000}, "video", "me")
	for _, track := range []*webrtc.TrackLocalStaticRTP{audio, video} {
		if _, err := pc.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly}); err != nil {
			t.Fatal(err)
		}
	}
	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate != nil {
			c.send(ctx, map[string]any{"candidate": candidate.ToJSON()})
		}
	})
	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		key := remote.StreamID() + "/" + remote.Kind().String()
		count := &atomic.Int64{}
		c.mu.Lock()
		c.received[key] = count
		c.mu.Unlock()
		go func() {
			for {
				if _, _, err := remote.ReadRTP(); err != nil {
					return
				}
				count.Add(1)
			}
		}()
	})
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for sequence := uint16(1); ; sequence++ {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			_ = audio.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: sequence, Timestamp: uint32(sequence) * 960}, Payload: []byte{0xfc, 0xff, 0xfe}})
			_ = video.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: sequence, Timestamp: uint32(sequence) * 1800, Marker: true}, Payload: []byte{0x10, 0x00, 0x9d, 0x01, 0x2a}})
		}
	}()
	go c.negotiate(ctx)

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	c.send(ctx, map[string]any{"description": pc.LocalDescription()})
	return c
}

func (c *forwardedClient) send(ctx context.Context, signal map[string]any) {
	message, _ := json.Marshal(map[string]any{"action": "signal", "call": c.callID, "to": forwardingUnit, "signal": signal})
	c.writing.Lock()
	defer c.writing.Unlock()
	_ = c.socket.Write(ctx, websocket.MessageText, message)
}

// negotiate answers what the unit sends over the live connection.
func (c *forwardedClient) negotiate(ctx context.Context) {
	for {
		_, data, err := c.socket.Read(ctx)
		if err != nil {
			return
		}
		var frame struct {
			Type    string `json:"type"`
			Payload struct {
				From   string `json:"from"`
				Signal struct {
					Description *webrtc.SessionDescription `json:"description"`
					Candidate   *webrtc.ICECandidateInit   `json:"candidate"`
				} `json:"signal"`
			} `json:"payload"`
		}
		if json.Unmarshal(data, &frame) != nil || frame.Type != "call.signal" || frame.Payload.From != forwardingUnit {
			continue
		}
		signal := frame.Payload.Signal
		switch {
		case signal.Description != nil && signal.Description.Type == webrtc.SDPTypeOffer:
			if c.pc.SetRemoteDescription(*signal.Description) != nil {
				continue
			}
			answer, _ := c.pc.CreateAnswer(nil)
			_ = c.pc.SetLocalDescription(answer)
			c.send(ctx, map[string]any{"description": c.pc.LocalDescription()})
		case signal.Description != nil:
			_ = c.pc.SetRemoteDescription(*signal.Description)
		case signal.Candidate != nil:
			_ = c.pc.AddICECandidate(*signal.Candidate)
		}
	}
}

// receiving counts the other people whose stream of a kind is arriving.
func (c *forwardedClient) receiving(kind string) int {
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

// sixInAChannel is a server with calling on and six members of one channel.
func sixInAChannel(t *testing.T) (http.Handler, string, string, []string) {
	t.Helper()
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	setFeature(t, handler, admin, "calls", true)
	room := makeRoom(t, handler, admin, "all-hands", "public")
	people := []string{}
	for index := range 6 {
		// From addresses of their own: registration is limited per address.
		name := fmt.Sprintf("person%d", index)
		if code := callFrom(t, handler, fmt.Sprintf("192.0.2.%d", 10+index), http.MethodPost, "/api/auth/register",
			map[string]string{"username": name, "password": testPassword}, "").Code; code != http.StatusCreated {
			t.Fatalf("register %s: %d", name, code)
		}
		person := signIn(t, handler, name, testPassword)
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/join", nil, person, http.StatusOK)
		people = append(people, person)
	}
	return handler, admin, room, people
}

/*
Without the forwarding unit, the mesh limit is the call's limit: the fifth
person is refused, and told it is because this server does not forward calls.
*/
func TestPastTheMeshLimitWithoutForwardingIsRefused(t *testing.T) {
	handler, _, room, people := sixInAChannel(t)
	for _, person := range people[:4] {
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, person, http.StatusOK)
	}
	refused := call(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, people[4])
	if refused.Code != http.StatusConflict || !strings.Contains(refused.Body.String(), "does not forward") {
		t.Errorf("the fifth person in a mesh: %d %s", refused.Code, refused.Body.String())
	}
	state := decode(t, mustCall(t, handler, http.MethodGet, "/api/rooms/"+room+"/call", nil, people[0], http.StatusOK))
	if state["mode"] != "mesh" {
		t.Errorf("a call of four is carried as %v", state["mode"])
	}
}

/*
With the unit on, six people hold one call: it moves to the unit when the fifth
joins, everybody is told, and each person, sending once, receives the other
five — negotiated over the live connection and authorised there.
*/
func TestSixPeopleHoldAForwardedCall(t *testing.T) {
	handler, admin, room, people := sixInAChannel(t)
	setSetting(t, handler, admin, "calls.sfu", "true")

	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sockets := []*websocket.Conn{}
	var callID string
	for index, person := range people {
		sockets = append(sockets, liveConnection(t, ctx, server, person))
		joined := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true, "video": true}, person, http.StatusOK))
		callID = joined["id"].(string)
		want := "mesh"
		if index >= 4 {
			want = "sfu"
		}
		if joined["mode"] != want {
			t.Fatalf("with %d people the call is %v, want %s", index+1, joined["mode"], want)
		}
	}

	clients := []*forwardedClient{}
	for _, socket := range sockets {
		clients = append(clients, newForwardedClient(t, ctx, socket, callID))
	}
	deadline := time.Now().Add(30 * time.Second)
	for index, client := range clients {
		for client.receiving("video") < 5 || client.receiving("audio") < 5 {
			if time.Now().After(deadline) {
				t.Fatalf("person %d receives %d cameras and %d voices, want 5 of each",
					index, client.receiving("video"), client.receiving("audio"))
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

/*
The unit hears only from people in the call. Somebody in the conversation but
not the call, addressing the unit, is refused and given nothing.
*/
func TestTheForwardingUnitRefusesSomebodyNotInTheCall(t *testing.T) {
	handler, admin, room, people := sixInAChannel(t)
	setSetting(t, handler, admin, "calls.sfu", "true")
	setSetting(t, handler, admin, "calls.mesh_limit", "1")

	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var callID string
	for _, person := range people[:2] {
		joined := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, person, http.StatusOK))
		callID = joined["id"].(string)
	}
	outsider := liveConnection(t, ctx, server, people[5])
	message, _ := json.Marshal(map[string]any{
		"action": "signal", "call": callID, "to": forwardingUnit,
		// A candidate, which the unit would hold without complaint for
		// anybody it heard from: only the authorisation can refuse it.
		"signal": map[string]any{"candidate": map[string]string{"candidate": "candidate:1 1 udp 1 127.0.0.1 9 typ host"}},
	})
	if err := outsider.Write(ctx, websocket.MessageText, message); err != nil {
		t.Fatal(err)
	}
	if _, refused := nextCallFrame(t, ctx, outsider, "error"); !refused {
		t.Error("somebody not in a forwarded call was not refused by its unit")
	}
}
