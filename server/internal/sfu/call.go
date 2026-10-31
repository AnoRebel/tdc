package sfu

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
	"uuid"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/cc"
	"github.com/pion/interceptor/pkg/gcc"
	"github.com/pion/interceptor/pkg/nack"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

/*
What each participant's connection carries from them, by the position of its
transceiver — the order the web client makes them in, which is the same order
a mesh connection uses: microphone, camera, shared screen, the screen's sound.
*/
var slots = []string{"microphone", "camera", "screen", "screen-audio"}

// streamOf is the stream a slot's track is sent to others in, "<user>:camera"
// or "<user>:screen", which is how a receiver tells whose it is and which.
func streamOf(user uuid.UUID, slot string) string {
	if slot == "screen" || slot == "screen-audio" {
		return user.String() + ":screen"
	}
	return user.String() + ":camera"
}

// How often each receiver's layers are chosen again from their bandwidth.
const choosingInterval = time.Second

// The least time between two requests for a key frame from one copy, so a
// receiver losing packets cannot make a sender produce nothing else.
const keyframeInterval = 500 * time.Millisecond

// call is one forwarded call on this node.
type call struct {
	id   uuid.UUID
	unit *Unit

	/*
		signals takes one participant's message at a time, so an offer and the
		candidates that follow it, arriving together, are applied in order.
	*/
	signals sync.Mutex
	// early holds candidates that arrived before their connection's offer.
	early map[uuid.UUID][]webrtc.ICECandidateInit

	mu    sync.Mutex
	peers map[uuid.UUID]*peer
}

// peer is one participant's connection to the unit.
type peer struct {
	call      *call
	user      uuid.UUID
	pc        *webrtc.PeerConnection
	estimator cc.BandwidthEstimator
	done      chan struct{}

	mu         sync.Mutex
	closed     bool
	pending    bool // a renegotiation is wanted once the current one settles
	candidates []webrtc.ICECandidateInit
	wants      map[uuid.UUID]Layer
	published  map[string]*publication
	receiving  map[*publication]*forwarder
}

// publication is one track a participant sends, in one copy or several.
type publication struct {
	owner *peer
	slot  string
	codec webrtc.RTPCodecCapability

	mu          sync.RWMutex
	layers      map[Layer]*webrtc.TrackRemote
	subscribers map[*forwarder]bool
	lastPLI     map[Layer]time.Time
}

// forwarder sends one publication to one receiver, one copy at a time.
type forwarder struct {
	receiver *peer
	from     *publication
	track    *webrtc.TrackLocalStaticRTP
	sender   *webrtc.RTPSender

	mu      sync.Mutex
	current Layer // the copy being sent; "-" before any
	target  Layer // the copy wanted
	started bool
	// The rewriting that makes several copies look like one stream to the
	// receiver: each copy numbers its packets and stamps its times from its
	// own start.
	seqOffset uint16
	tsOffset  uint32
	lastSeq   uint16
	lastTS    uint32
	lastAt    time.Time
}

const noLayer Layer = "-"

const maxEarlyCandidates = 32

func (c *call) handle(from uuid.UUID, signal Signal) error {
	c.signals.Lock()
	defer c.signals.Unlock()
	if signal.Reset {
		c.remove(from)
		delete(c.early, from)
		return nil
	}
	c.mu.Lock()
	p := c.peers[from]
	c.mu.Unlock()

	switch {
	case signal.Description != nil && signal.Description.Type == webrtc.SDPTypeOffer:
		if p != nil {
			return p.onOffer(*signal.Description)
		}
		created, err := c.connect(from)
		if err != nil {
			return err
		}
		created.candidates = c.early[from]
		delete(c.early, from)
		if err := created.onOffer(*signal.Description); err != nil {
			_ = created.pc.Close()
			return err
		}
		c.admit(created)
		return nil
	case p == nil && signal.Candidate != nil:
		if c.early == nil {
			c.early = map[uuid.UUID][]webrtc.ICECandidateInit{}
		}
		// A browser has a handful; anything past this many is not a browser.
		if len(c.early[from]) < maxEarlyCandidates {
			c.early[from] = append(c.early[from], *signal.Candidate)
		}
		return nil
	case p == nil:
		return errors.New("no connection to the forwarding unit yet; send an offer first")
	case signal.Description != nil && signal.Description.Type == webrtc.SDPTypeAnswer:
		return p.onAnswer(*signal.Description)
	case signal.Candidate != nil:
		return p.onCandidate(*signal.Candidate)
	case signal.Layers != nil:
		p.setWants(signal.Layers)
	}
	return nil
}

/*
newConnection makes a peer connection with a congestion controller of its own,
whose estimate of the bandwidth towards this participant is what their layers
are chosen within. Its pacer is a no-op: the estimate is read, and packets go
out as they come, as they would from a mesh.
*/
func (u *Unit) newConnection() (*webrtc.PeerConnection, cc.BandwidthEstimator, error) {
	media := &webrtc.MediaEngine{}
	feedback := []webrtc.RTCPFeedback{{Type: "nack"}, {Type: "nack", Parameter: "pli"}, {Type: "ccm", Parameter: "fir"}, {Type: "transport-cc"}}
	if err := media.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1",
		},
		PayloadType: 111,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, nil, err
	}
	// VP8 alone for video: every browser has it, and its key frames are
	// recognisable from a packet, which switching copies needs.
	if err := media.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000, RTCPFeedback: feedback},
		PayloadType:        96,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		return nil, nil, err
	}

	registry := &interceptor.Registry{}
	controller, err := cc.NewInterceptor(func() (cc.BandwidthEstimator, error) {
		return gcc.NewSendSideBWE(
			gcc.SendSideBWEInitialBitrate(1_000_000),
			gcc.SendSideBWEMaxBitrate(10_000_000),
			gcc.SendSideBWEPacer(gcc.NewNoOpPacer()),
		)
	})
	if err != nil {
		return nil, nil, err
	}
	estimators := make(chan cc.BandwidthEstimator, 1)
	controller.OnNewPeerConnection(func(_ string, estimator cc.BandwidthEstimator) { estimators <- estimator })
	registry.Add(controller)
	if err := webrtc.ConfigureTWCCHeaderExtensionSender(media, registry); err != nil {
		return nil, nil, err
	}
	// A quarter of the default history for resending lost packets: a second
	// or more of video at the sizes forwarded, and a quarter of the memory —
	// which, kept per stream sent, is most of what a forwarded call costs.
	if err := webrtc.RegisterDefaultInterceptorsWithOptions(media, registry,
		webrtc.WithNackResponderOptions(nack.ResponderSize(256))); err != nil {
		return nil, nil, err
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(media), webrtc.WithInterceptorRegistry(registry),
		webrtc.WithSettingEngine(u.settings))
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, nil, err
	}
	select {
	case estimator := <-estimators:
		return pc, estimator, nil
	default:
		return pc, nil, nil
	}
}

/*
connect makes somebody's connection to the unit. They are not given anybody's
tracks until their first offer has been answered: tracks added before it would
be matched against the transceivers that offer brings.
*/
func (c *call) connect(user uuid.UUID) (*peer, error) {
	pc, estimator, err := c.unit.newConnection()
	if err != nil {
		return nil, fmt.Errorf("make a connection: %w", err)
	}
	p := &peer{
		call: c, user: user, pc: pc, estimator: estimator, done: make(chan struct{}),
		wants: map[uuid.UUID]Layer{}, published: map[string]*publication{}, receiving: map[*publication]*forwarder{},
	}
	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		init := candidate.ToJSON()
		p.signal(Signal{Candidate: &init})
	})
	pc.OnTrack(p.onTrack)
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			c.removePeer(p)
		}
	})
	return p, nil
}

// admit puts a connected participant in the call and has them receive
// everything already being sent. Somebody connecting again replaces their
// last connection.
func (c *call) admit(p *peer) {
	c.mu.Lock()
	previous := c.peers[p.user]
	c.peers[p.user] = p
	others := make([]*peer, 0, len(c.peers))
	for _, each := range c.peers {
		if each != p {
			others = append(others, each)
		}
	}
	c.mu.Unlock()
	if previous != nil {
		c.removePeer(previous)
	}

	for _, other := range others {
		other.mu.Lock()
		publications := make([]*publication, 0, len(other.published))
		for _, each := range other.published {
			publications = append(publications, each)
		}
		other.mu.Unlock()
		for _, each := range publications {
			p.subscribe(each)
		}
	}
	go p.chooseLayersPeriodically()
}

func (p *peer) signal(signal Signal) {
	raw, err := json.Marshal(signal)
	if err != nil {
		return
	}
	p.call.unit.send(p.call.id, p.user, raw)
}

/*
onOffer answers the participant's offer — unless the unit's own offer is
outstanding, when the participant's is ignored: the unit is the impolite side,
and the participant's browser rolls its offer back on receiving the unit's and
makes it again afterwards.
*/
func (p *peer) onOffer(offer webrtc.SessionDescription) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	if p.pc.SignalingState() != webrtc.SignalingStateStable {
		return nil
	}
	if err := p.pc.SetRemoteDescription(offer); err != nil {
		return fmt.Errorf("take offer: %w", err)
	}
	p.flushCandidates()
	answer, err := p.pc.CreateAnswer(nil)
	if err != nil {
		return fmt.Errorf("make answer: %w", err)
	}
	if err := p.pc.SetLocalDescription(answer); err != nil {
		return fmt.Errorf("set answer: %w", err)
	}
	p.signal(Signal{Description: p.pc.LocalDescription()})
	if p.pending {
		p.negotiate()
	}
	return nil
}

func (p *peer) onAnswer(answer webrtc.SessionDescription) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.pc.SignalingState() != webrtc.SignalingStateHaveLocalOffer {
		return nil
	}
	if err := p.pc.SetRemoteDescription(answer); err != nil {
		return fmt.Errorf("take answer: %w", err)
	}
	p.flushCandidates()
	if p.pending {
		p.negotiate()
	}
	return nil
}

func (p *peer) onCandidate(candidate webrtc.ICECandidateInit) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pc.RemoteDescription() == nil {
		p.candidates = append(p.candidates, candidate)
		return nil
	}
	if err := p.pc.AddICECandidate(candidate); err != nil {
		// A candidate for a description since replaced is harmless.
		p.call.unit.log.Debug("forwarding unit ignored a candidate", "error", err)
	}
	return nil
}

func (p *peer) flushCandidates() {
	for _, candidate := range p.candidates {
		_ = p.pc.AddICECandidate(candidate)
	}
	p.candidates = nil
}

// negotiate offers the participant what has changed, or waits for the
// negotiation in progress to finish. Called with p.mu held.
func (p *peer) negotiate() {
	if p.closed {
		return
	}
	if p.pc.SignalingState() != webrtc.SignalingStateStable || p.pc.RemoteDescription() == nil {
		p.pending = true
		return
	}
	p.pending = false
	offer, err := p.pc.CreateOffer(nil)
	if err == nil {
		err = p.pc.SetLocalDescription(offer)
	}
	if err != nil {
		p.call.unit.log.Warn("forwarding unit could not renegotiate", "error", err)
		return
	}
	p.signal(Signal{Description: p.pc.LocalDescription()})
}

// slotOf is which of the participant's tracks a receiver carries, by its
// transceiver's position.
func (p *peer) slotOf(receiver *webrtc.RTPReceiver) (string, bool) {
	for index, transceiver := range p.pc.GetTransceivers() {
		if transceiver.Receiver() == receiver {
			if index < len(slots) {
				return slots[index], true
			}
			return "", false
		}
	}
	return "", false
}

// onTrack is a track, or one copy of one, arriving from the participant.
func (p *peer) onTrack(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	slot, ok := p.slotOf(receiver)
	if !ok {
		return
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	pub := p.published[slot]
	fresh := pub == nil
	if fresh {
		pub = &publication{
			owner: p, slot: slot, codec: remote.Codec().RTPCodecCapability,
			layers: map[Layer]*webrtc.TrackRemote{}, subscribers: map[*forwarder]bool{}, lastPLI: map[Layer]time.Time{},
		}
		p.published[slot] = pub
	}
	p.mu.Unlock()

	pub.mu.Lock()
	pub.layers[Layer(remote.RID())] = remote
	pub.mu.Unlock()

	if fresh {
		for _, other := range p.call.others(p) {
			other.subscribe(pub)
		}
	} else {
		for _, other := range p.call.others(p) {
			other.chooseLayers()
		}
	}
	go pub.read(remote)
}

func (c *call) others(p *peer) []*peer {
	c.mu.Lock()
	defer c.mu.Unlock()
	others := make([]*peer, 0, len(c.peers))
	for _, each := range c.peers {
		if each != p {
			others = append(others, each)
		}
	}
	return others
}

// read copies one track's packets to everybody receiving it, until it ends.
func (pub *publication) read(remote *webrtc.TrackRemote) {
	layer := Layer(remote.RID())
	for {
		packet, _, err := remote.ReadRTP()
		if err != nil {
			return
		}
		pub.mu.RLock()
		for each := range pub.subscribers {
			each.write(packet, layer)
		}
		pub.mu.RUnlock()
	}
}

// requestKeyframe asks the sender for a key frame of one copy, so a receiver
// starting or switching to it has a picture.
func (pub *publication) requestKeyframe(layer Layer) {
	pub.mu.Lock()
	remote := pub.layers[layer]
	if remote == nil || time.Since(pub.lastPLI[layer]) < keyframeInterval {
		pub.mu.Unlock()
		return
	}
	pub.lastPLI[layer] = time.Now()
	pub.mu.Unlock()
	_ = pub.owner.pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(remote.SSRC())}})
}

func (pub *publication) available() []Layer {
	pub.mu.RLock()
	defer pub.mu.RUnlock()
	layers := make([]Layer, 0, len(pub.layers))
	for layer := range pub.layers {
		layers = append(layers, layer)
	}
	return layers
}

func (pub *publication) simulcast() bool {
	for _, layer := range pub.available() {
		if layer != LayerSingle {
			return true
		}
	}
	return false
}

// subscribe has the participant receive another's track.
func (p *peer) subscribe(pub *publication) {
	track, err := webrtc.NewTrackLocalStaticRTP(pub.codec, pub.slot, streamOf(pub.owner.user, pub.slot))
	if err != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.receiving[pub] != nil {
		return
	}
	transceiver, err := p.pc.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
	if err != nil {
		p.call.unit.log.Warn("forwarding unit could not add a track", "error", err)
		return
	}
	f := &forwarder{receiver: p, from: pub, track: track, sender: transceiver.Sender(), current: noLayer, target: LayerSingle}
	p.receiving[pub] = f
	go f.readFeedback()
	pub.mu.Lock()
	pub.subscribers[f] = true
	pub.mu.Unlock()
	p.negotiate()
	go p.chooseLayers()
	if pub.codec.MimeType == webrtc.MimeTypeVP8 {
		go pub.requestKeyframe(LayerSingle)
	}
}

// readFeedback reads what the receiver says about a track — which the
// interceptors need read to work — and passes on its requests for a key frame.
func (f *forwarder) readFeedback() {
	for {
		packets, _, err := f.sender.ReadRTCP()
		if err != nil {
			return
		}
		for _, packet := range packets {
			switch packet.(type) {
			case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
				f.mu.Lock()
				layer := f.current
				if layer == noLayer {
					layer = f.target
				}
				f.mu.Unlock()
				f.from.requestKeyframe(layer)
			}
		}
	}
}

func (f *forwarder) setTarget(layer Layer) {
	f.mu.Lock()
	changed := f.target != layer
	f.target = layer
	waiting := f.current != layer
	f.mu.Unlock()
	if changed && waiting && layer != LayerOff {
		f.from.requestKeyframe(layer)
	}
}

/*
write sends one packet on, if it is of the copy being sent. A switch to another
copy waits for a key frame of it, and continues the numbering and the clock
where the last copy left off, so the receiver sees one unbroken stream.
*/
func (f *forwarder) write(packet *rtp.Packet, layer Layer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.target == LayerOff {
		return
	}
	if layer != f.current {
		if layer != f.target {
			return
		}
		if f.from.codec.MimeType == webrtc.MimeTypeVP8 && !vp8Keyframe(packet.Payload) {
			return
		}
		if f.started {
			f.seqOffset = f.lastSeq + 1 - packet.SequenceNumber
			elapsed := uint32(time.Since(f.lastAt).Seconds() * float64(f.from.codec.ClockRate))
			f.tsOffset = f.lastTS + max(elapsed, 1) - packet.Timestamp
		}
		f.current = layer
	}
	out := *packet
	// Header extensions are numbered per connection; the sender's numbering
	// means nothing on the receiver's.
	out.Extension = false
	out.Extensions = nil
	out.SequenceNumber = packet.SequenceNumber + f.seqOffset
	out.Timestamp = packet.Timestamp + f.tsOffset
	f.lastSeq, f.lastTS, f.lastAt, f.started = out.SequenceNumber, out.Timestamp, time.Now(), true
	_ = f.track.WriteRTP(&out)
}

func (p *peer) setWants(wants map[string]Layer) {
	p.mu.Lock()
	for raw, layer := range wants {
		if user, err := uuid.Parse(raw); err == nil && (layer == LayerOff || rank(layer) >= 0) {
			p.wants[user] = layer
		}
	}
	p.mu.Unlock()
	p.chooseLayers()
}

func (p *peer) chooseLayersPeriodically() {
	ticker := time.NewTicker(choosingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			p.chooseLayers()
		}
	}
}

// chooseLayers picks, for every camera this participant receives, the copy
// their wishes and their bandwidth allow.
func (p *peer) chooseLayers() {
	p.mu.Lock()
	var forwarders []*forwarder
	var choices []choice
	voices := 0
	for pub, f := range p.receiving {
		if pub.codec.MimeType != webrtc.MimeTypeVP8 {
			voices++
			continue
		}
		if !pub.simulcast() {
			continue
		}
		want, said := p.wants[pub.owner.user]
		if !said {
			want = LayerHalf
		}
		forwarders = append(forwarders, f)
		choices = append(choices, choice{want: want, available: pub.available()})
	}
	budget := 2_500_000
	if p.estimator != nil {
		budget = p.estimator.GetTargetBitrate()
	}
	p.mu.Unlock()
	for index, layer := range pickLayers(budget, voices, choices) {
		forwarders[index].setTarget(layer)
	}
}

// keepOnly disconnects everybody not listed.
func (c *call) keepOnly(present []uuid.UUID) {
	c.mu.Lock()
	var gone []*peer
	for user, each := range c.peers {
		if !slices.Contains(present, user) {
			gone = append(gone, each)
		}
	}
	c.mu.Unlock()
	for _, each := range gone {
		c.removePeer(each)
	}
}

func (c *call) remove(user uuid.UUID) {
	c.mu.Lock()
	p := c.peers[user]
	c.mu.Unlock()
	if p != nil {
		c.removePeer(p)
	}
}

// removePeer closes somebody's connection and stops sending what they sent.
func (c *call) removePeer(p *peer) {
	c.mu.Lock()
	if c.peers[p.user] == p {
		delete(c.peers, p.user)
	}
	c.mu.Unlock()

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	close(p.done)
	published := p.published
	receiving := p.receiving
	p.receiving = map[*publication]*forwarder{}
	p.mu.Unlock()

	for pub, f := range receiving {
		pub.mu.Lock()
		delete(pub.subscribers, f)
		pub.mu.Unlock()
	}
	for _, pub := range published {
		pub.mu.Lock()
		subscribers := pub.subscribers
		pub.subscribers = map[*forwarder]bool{}
		pub.mu.Unlock()
		for f := range subscribers {
			f.receiver.unsubscribe(pub, f)
		}
	}
	_ = p.pc.Close()
}

func (p *peer) unsubscribe(pub *publication, f *forwarder) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.receiving[pub] != f {
		return
	}
	delete(p.receiving, pub)
	if err := p.pc.RemoveTrack(f.sender); err == nil {
		p.negotiate()
	}
}
