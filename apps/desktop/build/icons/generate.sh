#!/bin/sh
# Renders every icon the platforms ask for from the two SVG sources.
#
# Run from apps/desktop after changing build/icons/*.svg:
#
#     sh build/icons/generate.sh
#
# The results are committed, so a checkout builds without rsvg-convert. This is
# only needed when the artwork changes.
set -eu

here=$(dirname "$0")
cd "$here/../.."

svg=build/icons/tdc.svg
symbolic=build/icons/tdc-symbolic.svg

command -v rsvg-convert >/dev/null || {
	echo "rsvg-convert is needed to render the icons (librsvg)" >&2
	exit 1
}

# What Wails builds the platform bundles from.
rsvg-convert -w 1024 -h 1024 "$svg" -o build/appicon.png

# Linux: the sizes a desktop environment looks for in an icon theme.
for size in 16 24 32 48 64 128 256 512; do
	mkdir -p "build/linux/icons/hicolor/${size}x${size}/apps"
	rsvg-convert -w "$size" -h "$size" "$svg" \
		-o "build/linux/icons/hicolor/${size}x${size}/apps/net.anorebel.tdc.png"
done

# And the scalable one, which is preferred where it exists.
mkdir -p build/linux/icons/hicolor/scalable/apps
cp "$svg" build/linux/icons/hicolor/scalable/apps/net.anorebel.tdc.svg

# The tray, in one colour for a panel whose background this cannot read.
mkdir -p build/linux/icons/hicolor/symbolic/apps
cp "$symbolic" build/linux/icons/hicolor/symbolic/apps/net.anorebel.tdc-symbolic.svg
for size in 16 22 24 32; do
	rsvg-convert -w "$size" -h "$size" "$symbolic" -o "build/icons/tray-${size}.png"
done

echo "icons rendered"
