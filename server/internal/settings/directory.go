package settings

import (
	"fmt"
	"strconv"
)

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

/*
How fast the directory may be read.

Searching and opening profiles are limited per person, and per address for
somebody not signed in, because the directory's promise is that it is a place
to look for somebody rather than a way to copy everybody. The page-size ceiling
stops the table being taken in one request; these stop it being walked quickly
in many.

The defaults are generous for a person and tight for a program: thirty a minute
is more than anybody browsing will reach, and a list of every profile at that
rate takes long enough to notice.
*/
const (
	DirectoryReadsPerMinute          Key = "directory.reads_per_minute"
	DirectoryAnonymousReadsPerMinute Key = "directory.anonymous_reads_per_minute"

	/*
		DirectoryEnquiriesPerHour is how many enquiries one person may send in
		an hour. Five is plenty for somebody looking for a developer and very
		little for somebody sending the same message to everybody listed,
		which is the thing the limit is for.
	*/
	DirectoryEnquiriesPerHour Key = "directory.enquiries_per_hour"
)

func init() {
	defineFeature(FeatureDirectory, false,
		"The developer and company directory, enquiries and engagements. Nothing is listed while this is off.")

	define(map[Key]definition{
		DirectoryReadsPerMinute: {
			fallback:    "30",
			description: "Directory searches and profile views allowed per person per minute.",
			validate:    atLeastOne,
		},
		DirectoryAnonymousReadsPerMinute: {
			fallback:    "10",
			description: "Directory searches and profile views allowed per address per minute, for people not signed in.",
			validate:    atLeastOne,
		},
		DirectoryEnquiriesPerHour: {
			fallback:    "5",
			description: "Directory enquiries one person may send per hour.",
			validate:    atLeastOne,
		},
	})
}

/*
atLeastOne accepts a positive whole number.

Not nonNegative: for a limit, zero reads as "no limit" elsewhere in the
settings, and a directory with no read limit is the enumeration the limit
exists to stop. An operator who wants the directory unreadable turns it off.
*/
func atLeastOne(value string) error {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fmt.Errorf("%w: expected a whole number of at least 1", ErrInvalidValue)
	}
	return nil
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
