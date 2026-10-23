package backup

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"filippo.io/age"
	"github.com/google/uuid"
)

/*
The archive, from the outside in:

	age (scrypt passphrase)
	  gzip
	    tar
	      manifest.json                  always first
	      tables/<part>/<table>.ndjson   one JSON object per row
	      files/<storage key>            an attachment's or preview's bytes
	      summary.json                   always last

Tables come in an order where every row's references come before it, apart
from a table's references to itself.
*/
const (
	// FormatName identifies an archive as this package's.
	FormatName = "tdc-backup"
	// FormatVersion changes when a reader of an older version could no longer
	// read what is written. A reader refuses a version it does not know.
	FormatVersion = 1

	manifestName = "manifest.json"
	summaryName  = "summary.json"
	tablesPrefix = "tables/"
	filesPrefix  = "files/"
)

// Errors a caller tells apart.
var (
	// ErrWrongPassphrase is a passphrase that does not open the archive. It
	// is never reported as corruption, which would send somebody looking for
	// a fault that is not there.
	ErrWrongPassphrase = errors.New("that passphrase does not open this archive")
	// ErrNotAnArchive is something that is not one of these archives.
	ErrNotAnArchive = errors.New("this is not a TDC backup archive")
	// ErrInvalid is a request that cannot be carried out as asked.
	ErrInvalid = errors.New("invalid request")
	// ErrTooLarge is an archive past backup.max_bytes.
	ErrTooLarge = errors.New("the archive is larger than this server allows")
	// ErrCancelled is a job stopped by somebody.
	ErrCancelled = errors.New("cancelled")
)

// MinPassphraseLength is the shortest passphrase accepted. Twelve characters
// of anything is past what an offline guess at scrypt's cost can cover.
const MinPassphraseLength = 12

// workFactor is scrypt's cost, as log2(N). age's own default; tests lower it.
var workFactor = 18

// CheckPassphrase refuses a passphrase too short to protect an archive.
func CheckPassphrase(passphrase string) error {
	if utf8.RuneCountInString(passphrase) < MinPassphraseLength {
		return fmt.Errorf("%w: a passphrase needs at least %d characters", ErrInvalid, MinPassphraseLength)
	}
	return nil
}

// Manifest describes an archive. It is the first thing in it.
type Manifest struct {
	Format        string    `json:"format"`
	FormatVersion int       `json:"formatVersion"`
	SchemaVersion uint      `json:"schemaVersion"`
	ServerVersion string    `json:"serverVersion"`
	ServerName    string    `json:"serverName"`
	CreatedAt     time.Time `json:"createdAt"`
	Parts         []Part    `json:"parts"`
	// Rooms is set when only some channels were taken.
	Rooms []uuid.UUID `json:"rooms,omitempty"`
}

// Selection is what the archive holds.
func (m Manifest) Selection() Selection { return Selection{Parts: m.Parts, Rooms: m.Rooms} }

// TableCount is how many rows of one table an archive holds.
type TableCount struct {
	Part  Part   `json:"part"`
	Table string `json:"table"`
	Rows  int64  `json:"rows"`
}

// Summary is what was written. It is the last thing in an archive.
type Summary struct {
	Tables    []TableCount `json:"tables"`
	Files     int64        `json:"files"`
	FileBytes int64        `json:"fileBytes"`
	// MissingFiles are attachments whose bytes were gone from storage when
	// the backup reached them — removed by retention while it ran, usually.
	MissingFiles []string `json:"missingFiles,omitempty"`
}

// archiveWriter layers tar over gzip over age.
type archiveWriter struct {
	encrypted io.WriteCloser
	gzip      *gzip.Writer
	tar       *tar.Writer
	modified  time.Time
}

func newArchiveWriter(out io.Writer, passphrase string, modified time.Time) (*archiveWriter, error) {
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, fmt.Errorf("backup: preparing encryption: %w", err)
	}
	recipient.SetWorkFactor(workFactor)

	encrypted, err := age.Encrypt(out, recipient)
	if err != nil {
		return nil, fmt.Errorf("backup: starting encryption: %w", err)
	}
	compressed := gzip.NewWriter(encrypted)
	return &archiveWriter{
		encrypted: encrypted,
		gzip:      compressed,
		tar:       tar.NewWriter(compressed),
		modified:  modified,
	}, nil
}

func (w *archiveWriter) add(name string, size int64, body io.Reader) error {
	if err := w.tar.WriteHeader(&tar.Header{
		Name:     name,
		Mode:     0o600,
		Size:     size,
		ModTime:  w.modified,
		Typeflag: tar.TypeReg,
		Format:   tar.FormatPAX,
	}); err != nil {
		return fmt.Errorf("backup: writing %s: %w", name, err)
	}
	written, err := io.Copy(w.tar, body)
	if err != nil {
		return fmt.Errorf("backup: writing %s: %w", name, err)
	}
	if written != size {
		return fmt.Errorf("backup: %s was %d bytes, expected %d", name, written, size)
	}
	return nil
}

func (w *archiveWriter) addJSON(name string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("backup: encoding %s: %w", name, err)
	}
	return w.add(name, int64(len(encoded)), strings.NewReader(string(encoded)))
}

// Close finishes every layer, innermost first. Until it returns nil the
// archive is not whole.
func (w *archiveWriter) Close() error {
	if err := w.tar.Close(); err != nil {
		return fmt.Errorf("backup: finishing the archive: %w", err)
	}
	if err := w.gzip.Close(); err != nil {
		return fmt.Errorf("backup: finishing compression: %w", err)
	}
	if err := w.encrypted.Close(); err != nil {
		return fmt.Errorf("backup: finishing encryption: %w", err)
	}
	return nil
}

func tableEntryName(part Part, table string) string {
	return tablesPrefix + string(part) + "/" + table + ".ndjson"
}

// Entry is one thing read from an archive after its manifest.
type Entry struct {
	// Part and Table are set for a table's rows.
	Part  Part
	Table string
	// Key is set for a file: its storage key.
	Key string
	// Summary is set for the closing summary.
	Summary *Summary
	Size    int64
	Body    io.Reader
}

// Reader reads an archive in order.
type Reader struct {
	Manifest Manifest
	tar      *tar.Reader
	gzip     *gzip.Reader
}

/*
Open decrypts an archive and reads its manifest.

A wrong passphrase is ErrWrongPassphrase; anything that is not an archive of
this format, or of a version this server does not read, is ErrNotAnArchive.
*/
func Open(in io.Reader, passphrase string) (*Reader, error) {
	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrWrongPassphrase, err)
	}

	decrypted, err := age.Decrypt(in, identity)
	if err != nil {
		var mismatch *age.NoIdentityMatchError
		if errors.As(err, &mismatch) {
			return nil, ErrWrongPassphrase
		}
		return nil, fmt.Errorf("%w: %w", ErrNotAnArchive, err)
	}

	decompressed, err := gzip.NewReader(decrypted)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotAnArchive, err)
	}

	reader := &Reader{tar: tar.NewReader(decompressed), gzip: decompressed}

	header, err := reader.tar.Next()
	if err != nil || header.Name != manifestName {
		return nil, fmt.Errorf("%w: it does not begin with a manifest", ErrNotAnArchive)
	}
	// A manifest is a few hundred bytes; anything far larger is not one.
	if err := json.NewDecoder(io.LimitReader(reader.tar, 1<<20)).Decode(&reader.Manifest); err != nil {
		return nil, fmt.Errorf("%w: the manifest cannot be read: %w", ErrNotAnArchive, err)
	}
	if reader.Manifest.Format != FormatName {
		return nil, fmt.Errorf("%w: its format is %q", ErrNotAnArchive, reader.Manifest.Format)
	}
	if reader.Manifest.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("%w: it is format version %d, and this server reads version %d",
			ErrNotAnArchive, reader.Manifest.FormatVersion, FormatVersion)
	}

	return reader, nil
}

// Next returns the next entry, or io.EOF after the summary.
func (r *Reader) Next() (Entry, error) {
	header, err := r.tar.Next()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return Entry{}, io.EOF
		}
		return Entry{}, fmt.Errorf("%w: %w", ErrNotAnArchive, err)
	}

	entry := Entry{Size: header.Size, Body: r.tar}
	switch {
	case header.Name == summaryName:
		var summary Summary
		if err := json.NewDecoder(io.LimitReader(r.tar, 64<<20)).Decode(&summary); err != nil {
			return Entry{}, fmt.Errorf("%w: the summary cannot be read: %w", ErrNotAnArchive, err)
		}
		entry.Summary = &summary
	case strings.HasPrefix(header.Name, tablesPrefix) && strings.HasSuffix(header.Name, ".ndjson"):
		rest := strings.TrimSuffix(strings.TrimPrefix(header.Name, tablesPrefix), ".ndjson")
		part, table, ok := strings.Cut(rest, "/")
		if !ok || !Known(Part(part)) {
			return Entry{}, fmt.Errorf("%w: unexpected entry %s", ErrNotAnArchive, header.Name)
		}
		entry.Part, entry.Table = Part(part), table
	case strings.HasPrefix(header.Name, filesPrefix):
		entry.Key = strings.TrimPrefix(header.Name, filesPrefix)
	default:
		return Entry{}, fmt.Errorf("%w: unexpected entry %s", ErrNotAnArchive, header.Name)
	}
	return entry, nil
}
