package places

import "testing"

func TestNearest(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lat, lon float64
		want     string
	}{
		{"central Nairobi", -1.286, 36.817, "Nairobi, Kenya"},
		{"Dar es Salaam", -6.80, 39.28, "Dar es Salaam, Tanzania"},
		{"Arusha", -3.37, 36.68, "Arusha, Tanzania"},
	} {
		got, ok := Nearest(tc.lat, tc.lon)
		if !ok || got != tc.want {
			t.Errorf("%s: got %q, %v; want %q", tc.name, got, ok, tc.want)
		}
	}
}

func TestNearestRefusesNowhere(t *testing.T) {
	// The middle of the Pacific is not near any town.
	if got, ok := Nearest(-30, -140); ok {
		t.Errorf("mid-ocean named %q", got)
	}
	for _, position := range [][2]float64{{91, 0}, {0, 181}, {-91, 0}} {
		if got, ok := Nearest(position[0], position[1]); ok {
			t.Errorf("%v is not a position but named %q", position, got)
		}
	}
}
