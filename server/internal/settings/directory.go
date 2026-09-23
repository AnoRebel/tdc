package settings

/*
The directory's switch.

Off unless an operator turns it on. A community server that only wants chat
should not discover one day that its members are being offered as developers
for hire; a directory is a decision about what the server is for, and that is
the operator's to make rather than to inherit.

Off means off everywhere at once, because every directory route sits behind
this one toggle: nothing is listed, nothing is searchable, no enquiry is
delivered. The data is not touched, so turning it back on needs no migration
and restores what was there.
*/
const FeatureDirectory Feature = "directory"

func init() {
	defineFeature(FeatureDirectory, false,
		"The developer and company directory, enquiries and engagements. Nothing is listed while this is off.")
}

// defineFeature adds a toggle from a feature's own file.
//
// A name defined twice panics rather than letting the later default win, for
// the reason define does.
func defineFeature(feature Feature, byDefault bool, description string) {
	if _, taken := featureDefaults[feature]; taken {
		panic("feature defined twice: " + string(feature))
	}
	featureDefaults[feature] = byDefault
	featureDescriptions[feature] = description
}
