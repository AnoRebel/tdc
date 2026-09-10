/*
Package credentials keeps what lets somebody stay signed in.

Two things per server: the session the server issued, and the password that
would obtain a new one. Storing both is what makes an expiring session
invisible — the client renews it rather than presenting a sign-in form every
few weeks — and it is also why where they are kept matters so much. A session
is revocable and expires on its own; a password is the account itself.

So they go in the operating system's keychain, which is encrypted at rest and
unlocked with the person's own login. Where there is no keychain, nothing is
written unless somebody has explicitly chosen an encrypted file and set a
passphrase for it. What never happens is a quiet fall back to something
weaker: the whole point of a keychain is that its absence is visible.
*/
package credentials

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/99designs/keyring"
)

/*
service is the name these entries appear under.

The reverse-domain identifier the application is packaged with, so somebody
looking through their keychain sees which application asked for it rather than
a bare word that could be anything.
*/
const service = "net.anorebel.tdc"

// ErrNoStore is returned when nothing on this machine can hold a credential.
var ErrNoStore = errors.New("no credential store")

/*
Credential is what is kept for one server.

Both fields are secret, and the distinction between them matters when one is
lost: a session can be revoked from another device and expires on its own,
where a password is the account until it is changed.
*/
type Credential struct {
	// Session is the cookie the server issued, as it would be sent back.
	Session string `json:"session"`
	/*
		Password obtains a new session when the old one expires.

		Kept so an expiry is invisible rather than a sign-in form appearing
		every few weeks. It is the reason this package refuses to write
		anywhere that is not encrypted.
	*/
	Password string `json:"password"`
	// Username identifies which account, since a server may have several.
	Username string `json:"username"`
}

/*
Store holds credentials, or explains why it cannot.

Kind says which backend is in use, so the interface can tell somebody what is
happening rather than leaving them to discover that nothing was remembered.
*/
type Store struct {
	ring keyring.Keyring
	kind Kind
}

// Kind is where credentials are being kept.
type Kind string

const (
	// KindKeychain is the operating system's own store, which is what this
	// wants: encrypted at rest and unlocked by the person's login.
	KindKeychain Kind = "keychain"
	/*
		KindEncryptedFile is a file encrypted with a passphrase somebody chose.

		Only ever used when it has been asked for. It is weaker than a keychain
		in one specific way — the passphrase is typed into this application
		rather than held by the operating system — and stronger than the
		alternative of not remembering anything, which is a judgement only the
		person using it can make.
	*/
	KindEncryptedFile Kind = "encrypted-file"
	// KindNone means nothing is stored and signing in happens every launch.
	KindNone Kind = "none"
)

/*
Open finds the operating system's keychain.

Returns ErrNoStore where there is none — a headless Linux machine with no
secret service running, most often. That is not a failure to hide: the caller
says so and offers the alternatives, because somebody who thinks they are being
remembered and is not will assume the application is broken.
*/
func Open() (*Store, error) {
	ring, err := keyring.Open(keyring.Config{
		ServiceName: service,

		/*
			The operating system's own stores, and nothing else.

			keyring's default list ends with a file backend that would be
			chosen automatically when no keychain is present — which is exactly
			the silent fall back to something weaker this package exists to
			prevent. The file backend is available through OpenEncryptedFile,
			where somebody has asked for it.
		*/
		AllowedBackends: []keyring.BackendType{
			keyring.WinCredBackend,
			keyring.KeychainBackend,
			keyring.SecretServiceBackend,
			keyring.KWalletBackend,
		},

		// macOS: this application's own keychain item, not the login
		// keychain's shared pool, so a prompt names TDC.
		KeychainName:                   "login",
		KeychainTrustApplication:       true,
		KeychainAccessibleWhenUnlocked: true,

		// Linux: the collection a desktop session unlocks at login.
		LibSecretCollectionName: "login",
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoStore, err)
	}

	return &Store{ring: ring, kind: KindKeychain}, nil
}

/*
OpenEncryptedFile opens a file encrypted with a passphrase.

Only for somebody who has been told there is no keychain and has chosen this
rather than signing in each time. The passphrase is not stored anywhere: losing
it means the file cannot be read, which is what makes it worth having.
*/
func OpenEncryptedFile(directory string, passphrase func() (string, error)) (*Store, error) {
	if directory == "" {
		return nil, errors.New("no directory was given for the encrypted file")
	}

	ring, err := keyring.Open(keyring.Config{
		ServiceName:      service,
		AllowedBackends:  []keyring.BackendType{keyring.FileBackend},
		FileDir:          directory,
		FilePasswordFunc: func(string) (string, error) { return passphrase() },
	})
	if err != nil {
		return nil, fmt.Errorf("the encrypted file could not be opened: %w", err)
	}

	return &Store{ring: ring, kind: KindEncryptedFile}, nil
}

// Kind reports where this store keeps things.
func (s *Store) Kind() Kind {
	if s == nil {
		return KindNone
	}
	return s.kind
}

/*
Save records the credential for one server.

Keyed by the server's checked origin, so two servers never share an entry and
removing one leaves the others alone.
*/
func (s *Store) Save(server string, credential Credential) error {
	if s == nil {
		return ErrNoStore
	}

	encoded, err := json.Marshal(credential)
	if err != nil {
		return fmt.Errorf("encoding the credential: %w", err)
	}

	return s.ring.Set(keyring.Item{
		Key:  server,
		Data: encoded,
		/*
			What somebody sees in their keychain application. Named rather
			than left blank so a person auditing their own keychain can tell
			what this is and remove it deliberately.
		*/
		Label:       "TDC — " + server,
		Description: "Session and password for a TDC server",
	})
}

/*
Load returns what is held for one server.

A missing entry is not an error: somebody who has not signed in to this server
has no credential, which is the ordinary case on a first launch.
*/
func (s *Store) Load(server string) (Credential, bool, error) {
	if s == nil {
		return Credential{}, false, ErrNoStore
	}

	item, err := s.ring.Get(server)
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return Credential{}, false, nil
	}
	if err != nil {
		return Credential{}, false, fmt.Errorf("reading the credential: %w", err)
	}

	var credential Credential
	if err := json.Unmarshal(item.Data, &credential); err != nil {
		/*
			Unreadable rather than absent. An entry written by an older version,
			or corrupted — reported rather than silently treated as "not signed
			in", because the difference matters when somebody is wondering why
			they keep being asked.
		*/
		return Credential{}, false, fmt.Errorf("the stored credential could not be read: %w", err)
	}

	return credential, true, nil
}

/*
Forget removes what is held for one server.

Called when somebody signs out or removes a server. A missing entry is success:
the intent is that nothing be held, and it is not.
*/
func (s *Store) Forget(server string) error {
	if s == nil {
		return ErrNoStore
	}

	err := s.ring.Remove(server)

	/*
		Absent is success either way it is reported. The keychain backends say
		ErrKeyNotFound; the file backend surfaces the underlying os.ErrNotExist,
		and treating that as a failure would make signing out twice look like
		something went wrong.
	*/
	if err == nil || errors.Is(err, keyring.ErrKeyNotFound) || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("removing the credential: %w", err)
}

/*
Advice explains what to do about a machine with no keychain.

Kept next to the code that discovers the problem, so the words somebody reads
and the condition that produced them cannot drift apart.
*/
func Advice() string {
	var advice strings.Builder

	advice.WriteString("TDC could not reach a keychain on this machine, so it cannot ")
	advice.WriteString("remember you between launches.\n\n")
	/*
		Not running is the common case rather than not installed — a desktop
		session normally starts one at login, and a machine reached over SSH
		or running a bare window manager often has none. Saying "install"
		alone sends somebody to install what they already have.
	*/
	advice.WriteString("On Linux this usually means the keyring service is not running. ")
	advice.WriteString("gnome-keyring or KDE Wallet provides one; starting it, or ")
	advice.WriteString("signing out and in again, is normally enough. Installing it is ")
	advice.WriteString("only needed if neither is present.\n\n")
	advice.WriteString("You can carry on without it — you will just have to sign in ")
	advice.WriteString("each time — or store your session in a file encrypted with a ")
	advice.WriteString("passphrase you choose.")

	return advice.String()
}
