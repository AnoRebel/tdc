# The desktop client

The same interface the browser shows, in a native window — plus the things a
browser will not do: notifications that survive the window being closed,
credentials in the operating system's keychain, a tray icon that keeps the
unread count in front of you.

Everything a person reads and types comes from `packages/ui`, the shared Nuxt
layer. Only how bytes reach the server differs between the two clients, so
they cannot drift apart by accident.

## Building it

You need [Go](https://go.dev) 1.27, [Bun](https://bun.sh), and the Wails CLI:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
wails3 doctor    # names anything missing for your platform
```

On Linux the webview is GTK and WebKitGTK. Debian and Ubuntu:

```sh
sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev
```

Then, from this directory:

```sh
bun install                       # once, from the repository root
bun run --cwd frontend build      # the frontend, which the binary embeds
go build .                        # the application
```

The order matters. The Go build embeds `frontend/dist`, so building the
binary before the frontend fails with a message about a missing directory
rather than anything about the frontend.

## Developing

```sh
wails3 dev
```

Which runs the Go application and the Nuxt dev server together, and reloads
the window when either changes. The frontend serves on the port in
`WAILS_VITE_PORT`, defaulting to 9245.

## What the binary contains

`main.go` embeds the built frontend with:

```go
//go:embed all:frontend/dist
```

The `all:` prefix is not decoration. Go's embed skips files whose names begin
with an underscore, and Nuxt writes its JavaScript to `_nuxt` and its
translations to `_locales` — so without it the binary carries an `index.html`
that references nothing it holds. That failure builds cleanly, ships, and
shows up as a window opening onto a blank page.

`embed_test.go` holds the build to it, so the prefix cannot be lost in an
edit without a test saying so:

```sh
go test ./...
```

## Choosing a server

TDC is self-hosted, so this application is installed once and pointed at
whichever server its owner belongs to — which the web client never has to ask,
being served by the server it talks to. The first screen is that question, and
`internal/api/address.go` decides which answers are acceptable.

One rule: **a server reached over plain HTTP must be on this machine.** The
session cookie and every message travel over that connection, and on a network
somebody else can see, plain HTTP hands all of it to anybody on the same wire.
Loopback is exempt because there is no wire — and refusing it would make trying
this out impossible without first arranging a certificate.

The check lives in `UseServer`, not in the interface. These are bindings a
program can call directly, so a check anywhere else is one that can be skipped.

A bare host becomes `https://`, because somebody typing the name of their
server has not mistyped it by leaving off the scheme. What it will not do is
silently upgrade an address written explicitly as `http://`: quietly changing
what somebody asked for is how people end up trusting the wrong thing.

### More than one

Somebody may belong to their community's server and their employer's. Each gets
its own cookie jar, so switching is picking a different session rather than
signing in again — and one server's cookie never reaches another, which a
shared jar would do. Removing a server takes its session with it: re-adding it
starts signed out rather than silently resuming, which on a shared machine is
the difference that matters.

## The tray

What makes closing the window different from quitting. The menu carries the
few things somebody wants without looking at the application: bring it back,
silence it for a while, go straight to a conversation, add a server or a
channel, or leave.

Every item does something. A tray menu listing what an application might one
day do teaches people not to open it.

The mute options are flat rather than a submenu, and deliberately so: this menu
is rebuilt whenever the unread count or the conversations change, and nested
items do not survive that round trip over D-Bus — they arrive as an empty
submenu. Three items at the top level cost a little height and actually work.

Muting offers fifteen minutes, an hour, or until tomorrow, and says how long is
left. A meeting is why somebody reaches for it, and an application offering only
"off" would leave them silent for a week without noticing. "Until tomorrow"
means local midnight rather than twenty-four hours, which would run through most
of the next working day.

### The icon

Two icons, from `build/icons/*.svg` — run `sh build/icons/generate.sh` after
changing the artwork, and commit the result so a checkout builds without
librsvg.

The tray one is a single colour, because a tray sits on a panel whose colour
this application cannot read. It is drawn in **white**, not `currentColor`:
`currentColor` has no value when an SVG is rendered on its own and falls back
to black, which on a dark panel is an invisible icon — and that is exactly what
shipped the first time, confirmed by reading the pixmap off the bus as black at
12% opacity.

The two bubbles are outline-and-solid rather than two solids, because at
sixteen pixels two solid shapes merge into a blob.

## One instance

Launching TDC again means "show me TDC", not "give me a second copy". Two
copies would hold two sessions, two trays and two sets of notifications for one
person, and their unread counts would disagree.

A second launch raises the first window and exits, so pressing a launcher icon
while the application is minimised does what somebody meant.

## Notifications

Two kinds, both shown for the same message. The interface has its own — a bell
with a count and a panel — and those work exactly as they do in a browser.
System notifications are the other kind: the ones that arrive when nobody is
looking at the application, which is most of the time and the reason for
installing it rather than keeping a tab open.

Both, because somebody who dismissed a banner without reading it has not read
it, and a count that left it out would be lying.

Permission is asked for when somebody turns notifications on, not at startup.
An application that asks before it has shown anybody anything is asking about
something they have no opinion on yet, and the usual answer to that is no —
which on some platforms cannot be undone from inside the application.

Pressing one opens the conversation it was about. The room and message travel
inside the notification and come back on activation, so one pressed an hour
later still knows where it was going without this keeping an index of pending
notifications correct.

Quiet hours are respected by not overriding them: notifications go out at
ordinary priority rather than time-sensitive, because whether a message is
worth interrupting for is a judgement the person made about their own
attention.

A second notification for one conversation replaces the first, so an hour away
does not produce forty banners for the same channel.

## Identity

`net.anorebel.tdc` — the bundle identifier, the keychain service name, and the
Linux program name.

The window class is not this. Wails builds a GTK application id of
`org.wails.<Name>` itself and does not expose it, so the window reports
`org.wails.tdc` and the desktop entry's `StartupWMClass` has to say so or the
running window will not associate with the launcher entry. Both are set
deliberately: when Wails allows the id to be configured, the three become one.

That value is case-sensitive and taken from a running window rather than
guessed — the application is named "TDC" and the class is lowercase.

## Staying signed in

Two things are kept per server: the session the server issued, and the password
that would obtain a new one. Storing both is what makes an expiring session
invisible — the client renews it rather than showing a sign-in form every few
weeks — and it is also why where they live matters. A session can be revoked
from another device and expires on its own; a password is the account until it
is changed.

So they go in the operating system's keychain: Keychain on macOS, Credential
Manager on Windows, Secret Service or KWallet on Linux. Encrypted at rest, and
unlocked by the person's own login.

The webview never sees them. `Recall` returns only the username, because a
script in the page reaching a password would defeat the point of a keychain
entirely — the Go side signs in with what it holds.

### When there is no keychain

Some machines have none: a bare window manager, a session reached over SSH, a
keyring service that is installed but not running. On those, **nothing is
written**. The application says so in words, offers to carry on signing in each
time, and offers a file encrypted with a passphrase for somebody who would
rather be remembered.

What never happens is a quiet fall back to something weaker. `keyring`'s own
default backend list ends with a file backend chosen automatically when no
keychain is present, which would write a password to disk on exactly the
machines least able to protect it — so this names the backends it will accept,
and a test reads that list to be sure a file backend has not crept back in.

The encrypted file is opt-in for a reason worth stating: its passphrase is
typed into this application rather than held by the operating system. That is
weaker than a keychain and stronger than not being remembered at all, and which
of those matters more is a judgement only the person using it can make.

## The menu

`internal/shell` builds the application menu. Items divide in two: those the
operating system owns — the clipboard, closing a window, quitting, full screen
— which Wails handles by role, and those the interface owns, which emit an
event the frontend answers in `useMenuCommands`.

Nothing in Go knows what a room is. A menu item announces that somebody asked
for something and the interface decides what that means, so the menu item and
the keyboard shortcut run the same code.

The event names are a contract across two languages. They are declared once in
Go and written to TypeScript by:

```sh
go generate ./internal/shell
```

The generated file is committed so a checkout builds without the tooling, and
`go test ./internal/shell` fails if the two sides have drifted apart.

Do not add an item that does nothing. A menu listing what an application might
one day do teaches people not to open it; items appear as the features land.

## How requests reach the server

Not over HTTP from the webview. The interface is served out of the binary, so
there is no origin to be same-origin with and no browser holding a cookie —
requests go through the generated bindings to `internal/api`, which keeps the
address and the session on the Go side.

Everything above that is shared. `app/lib/transport.ts` turns the binding into
the transport the shared `ApiClient` is built on, so every composable in the
layer calls `useApi()` and none of them knows which client it is running in.

The two adapters are held to one contract, in
`apps/web/app/lib/transport.test.ts`: each case is written once and run against
both. That is a different question from the tests in `packages/types`, which
cover the shared client given a transport that answers however they like —
this asks whether an HTTP client and a set of Wails bindings, which have
nothing in common underneath, present the same behaviour to everything above.
A case that needed different expectations per adapter would be a behaviour the
shared layer could not rely on.

Both transports are plain functions taking their dependency as an argument,
rather than importing it. The generated binding talks to a native host that
does not exist in a test process, and a transport that could only be exercised
with the application running would not be exercised.

### A server that was never there

A refusal is the server's answer and will not change; an unreachable server
might. The shared layer decides whether to retry or to keep somebody's work on
exactly that distinction, so both adapters have to express it the same way:
neither throws an `ApiError` — that means the server answered — and neither
attaches a status, which is what `readFailure` reads.

Go returns refusals as an answer with `ok` false and reports only genuine
unreachability as an error, which is why `Send` has both.

## Regenerating the bindings

`wails3 generate bindings -ts -i` writes TypeScript mirroring the Go services
into `frontend/bindings`, which the frontend reaches through the `#bindings`
alias. The frontend build regenerates them, so this is only needed by hand
when compiling Go without it.

## The version

`build/config.yml` carries the version shown on the About screen, and is
written from the `VERSION` file at the repository root by
`scripts/sync-version.ts`. Do not edit it by hand — two places claiming a
version is one place being wrong.

## Platforms

Built for Linux, macOS and Windows. Phones and tablets are the progressive
web application's job, so the mobile targets Wails scaffolds are not kept
here.
