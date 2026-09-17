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

## Starting, and where the window goes

Two choices, neither on unless somebody turns it on: open at login, and open to
the tray rather than to a window. An application that added itself to a login,
or opened invisibly, without being asked has taken a decision about somebody's
machine that is not its to take.

Opening hidden is read before the window is created, because a window created
visible and then hidden flashes on screen — which is exactly what somebody
choosing it asked not to happen.

The window's last position and size are remembered, and checked against the
connected displays before being used. A position remembered from a docked
laptop is off-screen on a train, and a window restored to it is one nobody can
see or reach.

What is checked is not whether the window fits but whether enough of it can be
**grabbed**: a strip along the top, which is what somebody actually takes hold
of, overlapping a screen by at least a third of the window's width. Judging by
total area accepts a window pushed up so far that only its lower half shows —
plenty of pixels on screen, and no title bar to drag it back by.

Saving is debounced by a second: dragging a window emits an event per frame,
and this is a file.

## Updates

Three separate steps, and deliberately so: checking says what exists,
downloading prepares it, and installing replaces what somebody is running.
Nothing moves to the next step on its own.

An artifact is installed only when a sigstore bundle proves it was built by
this project's release workflow. No trust root, no bundle, no matching
identity — nothing installed, and somebody told why. An updater that installed
what it could not verify would be a way to replace a running application with
whatever answered the request, wearing the appearance of a security feature.

**This build ships a placeholder trust root**, so updates are switched off: it
says so, offers to check anyway, and links to the releases page. That is honest
where silently doing nothing is not, and it changes when there is a release
pipeline to verify against.

Checking works whether or not installing does. Knowing a newer version exists
is the difference between updating by hand and not knowing there is anything to
update to.

How often is somebody's choice — on launch, daily, weekly, or only when asked —
because a check tells the release server that TDC is installed here and which
version. When the last check happened is kept by Go rather than the interface,
so settings arriving from the webview cannot claim a check that never was.

## Translucency

The window can show a little of what is behind it — where the platform allows
it. **Linux does not, today**, and the setting says so rather than appearing
broken.

Wails' `setTransparent()` is an empty function on its GTK4 path
(`pkg/application/linux_cgo.go`, commented "GTK4: Transparency via CSS -
different from GTK3"), so the window is never given a transparent background to
composite against. A minimal Wails application asking for alpha 128 is solid
too, which is how this was told apart from a fault here.

Everything on this side is in place for when that lands. The alpha is applied
when the window is created rather than afterwards, because WebKitGTK latches
its clear colour at the first composite and ignores later calls — which is why
the setting says it takes effect on reopening. The page paints the theme colour
_at_ that alpha rather than painting nothing: the shared stylesheet draws the
body with `var(--app-ground, var(--background))` and this client sets
`--app-ground` to a `color-mix` of the theme colour. Painting nothing was the
first attempt and it broke light mode, showing dark text on whatever WebKitGTK
clears to.

Only the body is tinted. Cards, dialogs and the sidebar keep their own opaque
surfaces, so text always sits on something solid.

Focus rings are drawn without the shared stylesheet's outline offset. That gap
shows the surface behind a control on a solid page, which is right — and shows
the _desktop_ through a translucent window, which makes the ring read as a
stray box floating beside what it marks.

## Asking first

Anything that loses something asks, and says what would be lost rather than
only whether to continue. "Are you sure?" is a question nobody can answer;
"the link stops working and whoever holds it cannot join" is.

Cancel takes focus, so a reflexive Enter is the safe answer.

Quitting is a request rather than an instruction. The menu item and the tray
both emit it, the interface checks whether anything is unsent and asks, and Go
ends the application only once it answers — neither side can decide alone,
because only one knows the state and only the other can act on it.

Closing the window asks once, the first time. For a chat client left open all
day, closing usually means "off my screen"; for somebody who opens it to read
something, it means done. Neither is guessable, so it asks and then does what
was chosen. Without a tray it always quits, whatever was chosen — hiding a
window with no icon to bring it back leaves an application somebody can neither
see nor stop.

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

`net.anorebel.tdc`, everywhere: the bundle identifier, the keychain service
name, the GTK application id, the Wayland surface's app_id, the name the icons
are installed under, and the desktop entry's `StartupWMClass`.

They have to agree. When they do not, a desktop environment cannot match the
running window to the entry that launched it — the application shows up as a
second, nameless icon, and pinning it to a dock pins something that never
lights up.

It was not always one name. Wails built the id as `org.wails.<Name>` and did
not expose it, so the window reported `org.wails.tdc` and `StartupWMClass` had
to say so too. Since beta.23 the id is an option, and all of it says the same
thing.

The value is case-sensitive, and worth reading off a running window rather than
assuming — `hyprctl clients` on Wayland, `xprop WM_CLASS` on X11. The
application is named "TDC"; the id is lowercase.

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

## Upgrading Wails

Three things move together, and all three must:

```sh
go get github.com/wailsapp/wails/v3@vX             # the Go side
go install github.com/wailsapp/wails/v3/cmd/wails3@vX   # the CLI
bun add --cwd frontend @wailsio/runtime@X          # the JavaScript side
```

The Go library and `@wailsio/runtime` are two halves of one bridge, released
together. When they disagree the application builds, starts, opens a window —
and the window is blank, with nothing in any log to say why. Upgrading the Go
side alone is the easy mistake, and it costs an hour to diagnose.

`internal/shell/runtime_test.go` compares the two and fails if they drift. It
also refuses a version range: `^3.0.0-beta.19` resolves to whatever npm feels
like, which is the same failure with an extra step.

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

## Cutting a release

Set `VERSION`, run `bun run version:sync`, commit, and push a tag that matches:

```sh
git tag v0.2.0 && git push origin v0.2.0
```

`.github/workflows/release.yml` does the rest. It refuses outright if the tag
and `VERSION` disagree — a binary that reports one version and is published as
another is worse than a release that did not happen.

What it produces, per platform: the binary, a sigstore bundle beside it, and
`latest.json` — the feed this client asks whether there is anything newer.

**Nothing is signed with a key.** GitHub vouches for the workflow through OIDC,
sigstore records the certificate in a public transparency log, and the client
checks that the certificate names this repository and that issuer. There is no
key to leak, rotate or lose, and no maintainer holds one. The client's half of
that contract is `certIdentityIssuer` and `certIdentitySANRegex` in
`internal/updater/verify.go`, and the two have to agree: renaming this
repository breaks verification until both change, which is the correct
outcome — an artifact from somewhere else should not install.

The feed carries no signature of its own, deliberately. A document vouching for
itself proves nothing; what makes an update trustworthy is the bundle beside the
artifact, checked after the download and before anything is written.

`internal/updater/testdata/workflow-feed.json` is the workflow's own output, and
a test parses it with the same types the client uses. Two languages describe
that document — Python writes it, Go reads it — and nothing else makes them
agree. Regenerate it if the workflow's shape changes.

### The trust root

`build/trust/sigstore-root.json` is still a placeholder, so this build verifies
nothing and installs nothing — it says so rather than pretending. Replace it
with sigstore's real trusted root before the first signed release, or every
update will be refused for a reason that has nothing to do with the artifact.

## Platforms

Built for Linux, macOS and Windows. Phones and tablets are the progressive
web application's job, so the mobile targets Wails scaffolds are not kept
here.

## If the window comes up blank on Linux

The window is drawn by WebKitGTK, and on NVIDIA's proprietary driver it can
come up blank — or die with "Error 71 (Protocol error)" — because that driver
fails to map the shared buffers WebKit asks for. It happens under both
Wayland and X11, so switching session type is not a cure.

Wails already handles the usual case: at startup it looks for
`/sys/module/nvidia`, and if that kernel module is loaded it turns the
DMA-BUF renderer off before WebKit starts. Nothing is required of you.

What that check misses is a card running the open-source nouveau driver,
which loads a differently named module. Whether nouveau needs the workaround
at all is unconfirmed — the underlying bug is described against the
proprietary driver. If you are on one of those cards and the window is blank,
set the switch yourself:

```sh
WEBKIT_DISABLE_DMABUF_RENDERER=1 tdc
```

It is honoured whatever the detection concluded, so it is also the way to
rule the renderer in or out when you are not sure what you are looking at.
Making it permanent means putting it in the environment the desktop entry
launches with, not in a shell profile, since nothing launched from a menu
reads those.

Both backends are otherwise equivalent, and are checked that way: the same
window rendered through Wayland and through XWayland produces the same
picture, pixel for pixel.
