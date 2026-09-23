package settings

import "testing"

// A setting defined twice refuses at startup, rather than the later
// definition's default and validator silently replacing the earlier's.
func TestASettingDefinedTwicePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("defining an existing setting again did not panic")
		}
	}()
	define(map[Key]definition{FederationEnabled: {fallback: "true"}})
}

// And a new one is added and readable as a known key.
func TestDefineAddsANewSetting(t *testing.T) {
	const probe Key = "test.define_probe"
	t.Cleanup(func() { delete(definitions, probe) })

	define(map[Key]definition{probe: {fallback: "x", description: "a probe"}})
	if !Known(probe) {
		t.Fatal("a defined setting is not known")
	}
}
