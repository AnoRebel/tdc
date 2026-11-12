package sfu

/*
Which simulcast layer each receiver gets.

A browser sending its camera in simulcast sends three copies at once: a quarter
size, half size and full size (rids q, h and f). The forwarding unit sends each
receiver one of them per person. Two things decide which:

  - what the receiver asked for, from how large it shows that person — a tile
    in a grid of nine does not need the full picture — and "off" for somebody
    not shown at all;
  - what the receiver's connection can take, from the congestion controller's
    estimate of the bandwidth towards them.

The receiver's wish is a ceiling and the bandwidth a budget. Within the budget
every person is given the most they were asked for; when the wishes do not fit,
the largest is stepped down first, so that a big picture of one speaker gives
way before a grid becomes unwatchable.
*/

// Layer names a simulcast copy: "q", "h", "f", or "" for a track sent in one
// copy only. Off is a wish not to receive a person at all.
type Layer string

const (
	LayerQuarter Layer = "q"
	LayerHalf    Layer = "h"
	LayerFull    Layer = "f"
	LayerSingle  Layer = ""
	LayerOff     Layer = "off"
)

// layerOrder is lowest first.
var layerOrder = []Layer{LayerQuarter, LayerHalf, LayerFull}

/*
layerCost is what each copy is assumed to take, in bits per second: the ceilings
the web client sets on its encodings. An estimate, not a measurement, which is
what a budget can be planned with before anything has been sent.
*/
var layerCost = map[Layer]int{LayerQuarter: 150_000, LayerHalf: 500_000, LayerFull: 1_500_000}

// audioAllowance is set aside from a receiver's budget for each voice they hear.
const audioAllowance = 50_000

func rank(layer Layer) int {
	for index, each := range layerOrder {
		if each == layer {
			return index
		}
	}
	return -1
}

// choice is one person's camera, as one receiver sees it.
type choice struct {
	want      Layer
	available []Layer // the copies the sender is producing, any order
}

/*
pickLayers chooses a copy for each choice within budget bits per second, once
an allowance is set aside for each of voices voices.

Never below the quarter layer: a picture the receiver asked for is shown small
rather than taken away, and the client turns a person off itself when it stops
showing them. A budget too small even for that is the congestion controller's
to handle, by the packets it lets through.
*/
func pickLayers(budget, voices int, choices []choice) []Layer {
	picked := make([]Layer, len(choices))
	for index, each := range choices {
		picked[index] = ceiling(each)
	}
	budget -= voices * audioAllowance
	for cost(picked) > budget {
		largest := -1
		for index, layer := range picked {
			if rank(layer) > 0 && (largest < 0 || rank(layer) > rank(picked[largest])) {
				largest = index
			}
		}
		if largest < 0 {
			break
		}
		picked[largest] = lowerAvailable(picked[largest], choices[largest].available)
	}
	return picked
}

// ceiling is the most a choice can have: what was asked, if it is being sent,
// or the next copy down that is.
func ceiling(each choice) Layer {
	if each.want == LayerOff {
		return LayerOff
	}
	want := rank(each.want)
	if want < 0 {
		want = rank(LayerHalf)
	}
	best := Layer("")
	for _, layer := range each.available {
		if r := rank(layer); r >= 0 && r <= want && (best == "" || r > rank(best)) {
			best = layer
		}
	}
	if best == "" {
		// Nothing at or below the wish: the smallest there is.
		for _, layer := range each.available {
			if r := rank(layer); r >= 0 && (best == "" || r < rank(best)) {
				best = layer
			}
		}
	}
	return best
}

func lowerAvailable(layer Layer, available []Layer) Layer {
	for r := rank(layer) - 1; r >= 0; r-- {
		for _, each := range available {
			if each == layerOrder[r] {
				return each
			}
		}
	}
	return layerOrder[0]
}

func cost(layers []Layer) int {
	total := 0
	for _, layer := range layers {
		total += layerCost[layer]
	}
	return total
}

/*
vp8Keyframe says whether an RTP packet starts a VP8 key frame: the only point
at which a receiver can be moved from one copy to another without a broken
picture. RFC 7741's payload descriptor, then the frame header's inverse key
frame bit.
*/
func vp8Keyframe(payload []byte) bool {
	if len(payload) < 1 {
		return false
	}
	extended := payload[0]&0x80 != 0
	start := payload[0]&0x10 != 0
	partition := payload[0] & 0x07
	if !start || partition != 0 {
		return false
	}
	at := 1
	if extended {
		if len(payload) < 2 {
			return false
		}
		flags := payload[1]
		at = 2
		if flags&0x80 != 0 { // picture identifier, one or two bytes
			if len(payload) <= at {
				return false
			}
			if payload[at]&0x80 != 0 {
				at += 2
			} else {
				at++
			}
		}
		if flags&0x40 != 0 { // TL0PICIDX
			at++
		}
		if flags&0x30 != 0 { // TID or KEYIDX
			at++
		}
	}
	return len(payload) > at && payload[at]&0x01 == 0
}
