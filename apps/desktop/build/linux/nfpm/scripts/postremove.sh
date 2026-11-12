#!/bin/sh

# The icons have gone, so the cache describing them must go with them;
# otherwise lookups keep resolving names to files that no longer exist.
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
  gtk-update-icon-cache -q -t -f /usr/share/icons/hicolor 2>/dev/null || true
fi

exit 0
