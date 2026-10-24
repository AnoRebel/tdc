package settings

import (
	"fmt"
	"strconv"
)

/*
Files: how large, how much, and for how long.

Settings rather than configuration, because what a community can afford to
store changes with its disk and its members rather than with a release, and an
operator who finds a limit too tight should not have to restart to loosen it.
*/
const (
	// FilesMaxFileBytes is the largest single file.
	FilesMaxFileBytes Key = "files.max_file_bytes"
	// FilesMaxMessageBytes is the largest total one message may carry.
	FilesMaxMessageBytes Key = "files.max_message_bytes"
	/*
		FilesQuotaPerPersonBytes is what one person's files may occupy, counted
		once per stored file and including their uploads in progress. Zero, the
		default, is no quota, as with the other limits.
	*/
	FilesQuotaPerPersonBytes Key = "files.quota_per_person_bytes"
	/*
		FilesUploadPieceBytes is the size of the pieces a client sends a file
		in. It exists for reverse proxies: nginx refuses a body over 1 MiB
		unless told otherwise, and an operator who cannot change the proxy can
		make the pieces fit it instead.
	*/
	FilesUploadPieceBytes Key = "files.upload_piece_bytes"
	/*
		FilesAbandonedAfterHours is how long an unfinished upload, or a file
		uploaded and never sent, is kept before its storage is released.
	*/
	FilesAbandonedAfterHours Key = "files.abandoned_after_hours"
	/*
		FilesFederationCopyMaxBytes is the largest file this server copies from
		another when one of its members opens it. Zero is no limit.
	*/
	FilesFederationCopyMaxBytes Key = "files.federation_copy_max_bytes"
	/*
		FilesDirectTransferRelay lets a direct transfer use the TURN relay when
		two devices cannot reach each other. A relayed transfer passes through
		the relay twice, so an operator paying for its traffic may keep it for
		calls alone.
	*/
	FilesDirectTransferRelay Key = "files.direct_transfer_relay"
)

// FeatureDirectTransfers lets two people send a file straight between their
// devices, without it passing through or resting on this server.
const FeatureDirectTransfers Feature = "direct-transfers"

// Piece sizes a client may be told to use: small enough for a strict proxy,
// large enough that a gigabyte is not ten thousand requests.
const (
	MinUploadPieceBytes = 256 << 10
	MaxUploadPieceBytes = 8 << 20
)

func init() {
	defineFeature(FeatureDirectTransfers, true,
		"Sending a file straight to another person's device, in a direct conversation, without it passing through this server.")

	define(map[Key]definition{
		FilesMaxFileBytes: {
			fallback:    strconv.Itoa(100 << 20),
			description: "The largest single file anybody may send, in bytes.",
			validate:    atLeastOne,
		},
		FilesMaxMessageBytes: {
			fallback:    strconv.Itoa(250 << 20),
			description: "The largest total of files one message may carry, in bytes.",
			validate:    atLeastOne,
		},
		FilesQuotaPerPersonBytes: {
			fallback:    "0",
			description: "What one person's files may occupy, in bytes, or 0 for no quota.",
			validate:    nonNegative,
		},
		FilesUploadPieceBytes: {
			fallback:    strconv.Itoa(4 << 20),
			description: "The size of the pieces files are uploaded in, in bytes (256 KiB to 8 MiB). Keep it under your proxy's body limit.",
			validate:    between(MinUploadPieceBytes, MaxUploadPieceBytes),
		},
		FilesAbandonedAfterHours: {
			fallback:    "24",
			description: "Hours after which an unfinished upload, or a file uploaded and never sent, is removed.",
			validate:    atLeastOne,
		},
		FilesFederationCopyMaxBytes: {
			fallback:    strconv.Itoa(100 << 20),
			description: "The largest file this server copies from another server when a member opens it, in bytes, or 0 for no limit.",
			validate:    nonNegative,
		},
		FilesDirectTransferRelay: {
			fallback:    "true",
			description: "Whether direct transfers may use the TURN relay when two devices cannot reach each other.",
			validate:    boolean,
		},
	})
}

// between accepts a whole number within bounds.
func between(low, high int64) func(string) error {
	return func(value string) error {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < low || parsed > high {
			return fmt.Errorf("%w: expected a whole number from %d to %d", ErrInvalidValue, low, high)
		}
		return nil
	}
}
