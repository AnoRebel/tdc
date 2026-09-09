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
