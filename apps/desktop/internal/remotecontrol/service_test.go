package remotecontrol

import (
	"errors"
	"fmt"
	"testing"
)

type recorded struct {
	calls    []string
	watching int
}

func (r *recorded) Physical() bool { return true }
func (r *recorded) Move(x, y int) error {
	r.calls = append(r.calls, fmt.Sprintf("move %d,%d", x, y))
	return nil
}
func (r *recorded) Button(button int, down bool) error {
	r.calls = append(r.calls, fmt.Sprintf("button %d %v", button, down))
	return nil
}
func (r *recorded) Wheel(dx, dy int) error {
	r.calls = append(r.calls, fmt.Sprintf("wheel %d,%d", dx, dy))
	return nil
}
func (r *recorded) Key(code string, down bool) error {
	r.calls = append(r.calls, fmt.Sprintf("key %s %v", code, down))
	return nil
}
func (r *recorded) WatchRevokeKey(func()) func() {
	r.watching++
	return func() { r.watching-- }
}

func controlled(available bool) (*Service, *recorded) {
	injector := &recorded{}
	service := New(
		func() Capability { return Capability{Available: available} },
		func() (Injector, error) { return injector, nil },
		func() []Screen {
			return []Screen{
				{Physical: Rect{0, 0, 1920, 1080}, Primary: true},
				{Physical: Rect{1920, 0, 2560, 1440}},
			}
		},
	)
	return service, injector
}

var move = Event{Kind: "move", X: 0.5, Y: 0.5}

// No input reaches the system without a grant in force: before one, from
// anybody else, after revoking, after the share ends, and after somebody
// else is given control.
func TestNoInputIsAppliedWithoutAGrant(t *testing.T) {
	service, injector := controlled(true)

	if err := service.Grant("amina", "Amina", ""); !errors.Is(err, ErrNotSharing) {
		t.Errorf("granting with nothing shared: %v", err)
	}
	service.BeginShare(2560, 1440)
	if err := service.Apply("amina", move); !errors.Is(err, ErrNoGrant) {
		t.Errorf("input before a grant: %v", err)
	}

	if err := service.Grant("amina", "Amina", ""); err != nil {
		t.Fatal(err)
	}
	if err := service.Apply("baraka", move); !errors.Is(err, ErrNoGrant) {
		t.Errorf("input from somebody not given control: %v", err)
	}
	if err := service.Apply("amina", move); err != nil {
		t.Fatalf("input with control given: %v", err)
	}
	// The second screen is the one that size, and the middle of it is here.
	if injector.calls[0] != "move 3199,719" {
		t.Errorf("the pointer went to %s", injector.calls[0])
	}

	service.Revoke()
	if err := service.Apply("amina", move); !errors.Is(err, ErrNoGrant) {
		t.Errorf("input after revoking: %v", err)
	}

	_ = service.Grant("amina", "Amina", "")
	_ = service.Grant("baraka", "Baraka", "")
	if err := service.Apply("amina", move); !errors.Is(err, ErrNoGrant) {
		t.Errorf("input from somebody whose control passed to another: %v", err)
	}

	service.EndShare()
	if err := service.Apply("baraka", move); !errors.Is(err, ErrNoGrant) {
		t.Errorf("input after the share ended: %v", err)
	}
	if injector.watching != 0 {
		t.Errorf("the revoke key is still watched with no control given")
	}
}

// Where the platform cannot be driven, control is never given.
func TestControlIsRefusedWhereItCannotBeGiven(t *testing.T) {
	service, injector := controlled(false)
	service.BeginShare(1920, 1080)
	if err := service.Grant("amina", "Amina", ""); !errors.Is(err, ErrUnavailable) {
		t.Errorf("granting where input cannot be injected: %v", err)
	}
	if err := service.Apply("amina", move); !errors.Is(err, ErrNoGrant) || len(injector.calls) != 0 {
		t.Errorf("input applied where control cannot be given: %v %v", err, injector.calls)
	}
}

// The sharer's key is never pressed for them, and ending control lets go of
// whatever the controller held down.
func TestTheRevokeKeyIsTheSharersAlone(t *testing.T) {
	service, injector := controlled(true)
	service.BeginShare(1920, 1080)
	_ = service.Grant("amina", "Amina", "")
	if injector.watching != 1 {
		t.Error("the revoke key is not watched while control is given")
	}
	for _, modifier := range revokeChord.modifiers {
		_ = service.Apply("amina", Event{Kind: "key", Code: modifier + "Left", Down: true})
	}
	if err := service.Apply("amina", Event{Kind: "key", Code: revokeChord.code, Down: true}); !errors.Is(err, ErrReserved) {
		t.Errorf("the controller pressed the sharer's key: %v", err)
	}
	for _, call := range injector.calls {
		if call == "key "+revokeChord.code+" true" {
			t.Error("the sharer's key reached the system")
		}
	}

	injector.calls = nil
	_ = service.Apply("amina", Event{Kind: "key", Code: "ShiftLeft", Down: true})
	service.Revoke()
	released := false
	for _, call := range injector.calls {
		released = released || call == "key ShiftLeft false"
	}
	if !released {
		t.Errorf("revoking left keys held down: %v", injector.calls)
	}
}
