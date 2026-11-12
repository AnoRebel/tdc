package storage

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// S3Config is what either S3-backed store needs to reach a bucket.
//
// Shared between the two implementations so that an operator's configuration
// means the same thing whichever client is selected, and so that switching
// between them is a one-word change rather than a rewrite.
type S3Config struct {
	// Endpoint is the object store's address, e.g. "https://s3.example.org" or
	// "http://garage:3900". Required: TDC targets self-hosted object stores, so
	// there is no sensible default to fall back to.
	Endpoint string

	Bucket    string
	Region    string
	AccessKey string
	SecretKey string

	// UsePathStyle addresses buckets as endpoint/bucket rather than
	// bucket.endpoint. Most self-hosted stores need this, and a bare IP or a
	// name without dots cannot do virtual-host style at all.
	UsePathStyle bool
}

// Validate reports every problem at once.
//
// Configuration mistakes come in groups — a missing key usually means a missing
// secret too — and reporting them one restart at a time wastes an operator's
// evening.
func (c S3Config) Validate() error {
	var problems []string

	if c.Endpoint == "" {
		problems = append(problems, "endpoint is required")
	} else if parsed, err := url.Parse(c.Endpoint); err != nil {
		problems = append(problems, fmt.Sprintf("endpoint %q is not a URL: %v", c.Endpoint, err))
	} else if parsed.Scheme != "http" && parsed.Scheme != "https" {
		problems = append(problems, fmt.Sprintf("endpoint %q must be http or https", c.Endpoint))
	} else if parsed.Host == "" {
		problems = append(problems, fmt.Sprintf("endpoint %q names no host", c.Endpoint))
	}

	if c.Bucket == "" {
		problems = append(problems, "bucket is required")
	}
	if c.AccessKey == "" {
		problems = append(problems, "access key is required")
	}
	if c.SecretKey == "" {
		problems = append(problems, "secret key is required")
	}

	if len(problems) > 0 {
		return fmt.Errorf("storage: object store configuration: %s", strings.Join(problems, "; "))
	}
	return nil
}

// hostAndTLS splits an endpoint URL into the host and whether to use TLS, which
// is the form minio-go wants.
func (c S3Config) hostAndTLS() (string, bool, error) {
	parsed, err := url.Parse(c.Endpoint)
	if err != nil {
		return "", false, fmt.Errorf("storage: parsing endpoint %q: %w", c.Endpoint, err)
	}
	if parsed.Host == "" {
		return "", false, errors.New("storage: endpoint names no host")
	}
	return parsed.Host, parsed.Scheme == "https", nil
}

// contentDisposition builds the header that names a download.
//
// A key is a random identifier, so without this a browser saves the file under
// a meaningless name. The filename is quoted and stripped of anything that
// would let it break out of the header or steer where the file lands.
func contentDisposition(name string) string {
	if name == "" {
		return ""
	}

	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r < 0x20, r == 0x7f: // control characters, including CR and LF
			return -1
		case r == '"', r == '\\', r == '/':
			return -1
		default:
			return r
		}
	}, name)

	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return ""
	}

	return fmt.Sprintf("attachment; filename=%q", cleaned)
}
