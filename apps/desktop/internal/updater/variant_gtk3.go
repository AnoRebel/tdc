//go:build gtk3

package updater

/*
The legacy build runs on distributions without GTK4, so it must never be
offered the ordinary Linux artifact — that binary links libgtk-4 and would not
start on the machine that needed this one.

Asking the feed for a key of its own is what keeps them apart. A release that
stops publishing the legacy build therefore offers this machine nothing, which
is the honest answer: there is no newer version it can run.
*/
const platformVariant = "gtk3"
