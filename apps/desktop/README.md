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

## The version

`build/config.yml` carries the version shown on the About screen, and is
written from the `VERSION` file at the repository root by
`scripts/sync-version.ts`. Do not edit it by hand — two places claiming a
version is one place being wrong.

## Platforms

Built for Linux, macOS and Windows. Phones and tablets are the progressive
web application's job, so the mobile targets Wails scaffolds are not kept
here.
