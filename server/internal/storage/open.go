package storage

import (
	"fmt"
	"strings"
)

// Backend names a storage implementation in configuration.
type Backend string

const (
	// BackendLocal keeps files in a directory. The default, so that a small
	// deployment needs no object store at all.
	BackendLocal Backend = "local"

	// BackendS3AWS uses an S3-compatible store through the official AWS SDK.
	BackendS3AWS Backend = "s3-aws"

	// BackendS3Minio uses an S3-compatible store through minio-go.
	BackendS3Minio Backend = "s3-minio"
)

// Config selects and configures a store.
type Config struct {
	// Backend defaults to BackendLocal when empty.
	Backend Backend

	// Directory is where BackendLocal keeps its files.
	Directory string

	// S3 configures either S3-backed backend. Ignored by BackendLocal.
	S3 S3Config
}

// Open returns the store an operator asked for.
//
// The two S3 backends differ only in which client library they use: both speak
// to any S3-compatible store, both pass the same conformance tests, and the
// configuration means the same thing to each. An operator picks whichever they
// would rather have in the dependency tree, and can change their mind by
// editing one word.
func Open(config Config) (Store, error) {
	switch backend := config.Backend; backend {
	case "", BackendLocal:
		return NewLocal(config.Directory)

	case BackendS3AWS:
		return NewAWS(config.S3)

	case BackendS3Minio:
		return NewMinio(config.S3)

	default:
		return nil, fmt.Errorf(
			"storage: unknown backend %q; expected one of %s",
			backend, strings.Join([]string{
				string(BackendLocal), string(BackendS3AWS), string(BackendS3Minio),
			}, ", "),
		)
	}
}
