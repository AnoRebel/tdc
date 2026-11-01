package httpapi

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/settings"
	"github.com/labstack/echo/v5"
)

/*
What somebody chose the interface to look like.

Three independent choices — the palette, light or dark, and the accent — kept
on the account rather than only in a browser, so the choice follows somebody
between clients. An empty value in any of them means they have chosen nothing
there and take the operator's default; the client resolves that, because the
operator's default can change while somebody is signed in and their own choice
should not have to be rewritten when it does.
*/

type appearance struct {
	// Palette names a set of colours, one of settings.Palettes, or empty for
	// whatever the operator chose.
	Palette string `json:"palette"`
	// Mode is "light", "dark", "auto", or empty. Auto is a choice rather than
	// the absence of one — somebody who set dark on a device that is currently
	// light meant dark, and somebody on auto meant to follow the device.
	Mode string `json:"mode"`
	// Accent is a CSS colour, or empty for the palette's own.
	Accent string `json:"accent"`
}

var (
	knownModes = map[string]bool{"": true, "light": true, "dark": true, "auto": true}
)

// handleGetAppearance reports what this person chose.
func (s *Server) handleGetAppearance(c *echo.Context) error {
	user, _ := currentUser(c)

	row, err := s.queries.GetAppearance(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, appearance{
		Palette: row.AppearancePalette,
		Mode:    row.AppearanceMode,
		Accent:  row.AppearanceAccent,
	})
}

/*
handleSetAppearance records it.

Validated here rather than trusted, because this is an API a program calls the
same way a person's browser does: a palette name nothing matches would leave
somebody with an attribute selecting no rules, which looks like the default and
gives no reason why.
*/
func (s *Server) handleSetAppearance(c *echo.Context) error {
	user, _ := currentUser(c)

	var request appearance
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	request.Palette = strings.TrimSpace(request.Palette)
	request.Mode = strings.TrimSpace(request.Mode)
	request.Accent = strings.TrimSpace(request.Accent)

	if !settings.KnownPalette(request.Palette) {
		return echo.NewHTTPError(http.StatusBadRequest,
			fmt.Sprintf("%q is not a palette this server has", request.Palette))
	}
	if !knownModes[request.Mode] {
		return echo.NewHTTPError(http.StatusBadRequest,
			fmt.Sprintf("%q is not an appearance; use light, dark or auto", request.Mode))
	}

	/*
		The accent has to be readable, not merely well-formed.

		Syntax alone would accept a pale yellow that leaves every button's
		label at 1.5:1 — a setting that ruins the interface for the person who
		chose it and, if an operator sets it, for everybody. The refusal
		carries the ratio, because "that colour will not do" is not something
		somebody can act on.
	*/
	if request.Accent != "" {
		if err := readableAccent(request.Accent); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
	}

	if err := s.queries.SetAppearance(c.Request().Context(), dbgen.SetAppearanceParams{
		ID:                user.ID,
		AppearancePalette: request.Palette,
		AppearanceMode:    request.Mode,
		AppearanceAccent:  request.Accent,
	}); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, request)
}

// aaContrast is what WCAG asks of normal text.
const aaContrast = 4.5

/*
readableAccent reports whether a readable accent can be made from this colour.

The client does not apply a chosen colour directly. A single value cannot be
readable in both appearances — light mode draws near-white text on the accent
and dark mode near-black, so a shade dark enough for one is too light for the
other, and no value clears 4.5:1 against both. What it applies is a pair
derived from the colour: the same hue, with the lightness moved until each
appearance has a shade its own text can be read on.

So what this checks is that such a pair exists, which is what the client will
do with the value. Requiring the colour itself to clear both would refuse every
colour including the palettes' own, and requiring it to clear either would
accept colours the client then could not use.
*/
func readableAccent(colour string) error {
	accent, ok := parseColour(colour)
	if !ok {
		return errors.New("that colour could not be read; use a hex value like #4f46e5, " +
			"or an oklch() colour")
	}

	base := rgbToOklch(accent)

	lightForeground, _ := parseColour("oklch(0.99 0 0)")
	darkForeground, _ := parseColour("oklch(0.16 0.01 265)")

	// Light mode wants a darker shade, dark mode a lighter one.
	if !shadeExists(base, lightForeground, -1) || !shadeExists(base, darkForeground, +1) {
		return errors.New("no shade of that colour can be read in both light and dark; " +
			"try one with more colour in it — a near-grey has nowhere to move to")
	}

	return nil
}

// shadeExists reports whether moving this colour's lightness in one direction
// reaches something the given foreground can be read on.
func shadeExists(base oklch, foreground rgb, direction float64) bool {
	for offset := 0.0; offset <= 1; offset += 0.01 {
		lightness := base.lightness + direction*offset
		if lightness < 0 || lightness > 1 {
			return false
		}
		if contrastRatio(oklchToRGB(lightness, base.chroma, base.hue), foreground) >= aaContrast {
			return true
		}
	}
	return false
}

// oklch is a colour in a space where lightness can be moved without the hue.
type oklch struct{ lightness, chroma, hue float64 }

// rgbToOklch is the inverse of oklchToRGB, by the same steps in reverse.
func rgbToOklch(colour rgb) oklch {
	linear := func(channel float64) float64 {
		if channel <= 0.04045 {
			return channel / 12.92
		}
		return math.Pow((channel+0.055)/1.055, 2.4)
	}

	lr, lg, lb := linear(colour.r), linear(colour.g), linear(colour.b)

	l := math.Cbrt(0.4122214708*lr + 0.5363325363*lg + 0.0514459929*lb)
	m := math.Cbrt(0.2119034982*lr + 0.6806995451*lg + 0.1073969566*lb)
	s := math.Cbrt(0.0883024619*lr + 0.2817188376*lg + 0.6299787005*lb)

	lightness := 0.2104542553*l + 0.793617785*m - 0.0040720468*s
	a := 1.9779984951*l - 2.428592205*m + 0.4505937099*s
	b := 0.0259040371*l + 0.7827717662*m - 0.808675766*s

	hue := math.Atan2(b, a) * 180 / math.Pi
	if hue < 0 {
		hue += 360
	}

	return oklch{lightness: lightness, chroma: math.Hypot(a, b), hue: hue}
}

// rgb holds a colour's sRGB components, each 0–1.
type rgb struct{ r, g, b float64 }

var (
	hexColour   = regexp.MustCompile(`^#?([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
	oklchColour = regexp.MustCompile(`^oklch\(\s*([\d.]+%?)\s+([\d.]+)\s+([\d.]+)`)
)

/*
parseColour reads the notations this application actually produces.

oklch because the palettes are written in it, and hex because that is what a
colour picker gives. Anything else is refused rather than guessed at: a colour
that cannot be measured cannot be checked, and approving it unmeasured would
defeat the point of checking at all.
*/
func parseColour(value string) (rgb, bool) {
	text := strings.ToLower(strings.TrimSpace(value))

	if match := hexColour.FindStringSubmatch(text); match != nil {
		digits := match[1]
		if len(digits) == 3 {
			digits = string([]byte{
				digits[0], digits[0], digits[1], digits[1], digits[2], digits[2],
			})
		}
		var channels [3]float64
		for i := range channels {
			n, err := strconv.ParseUint(digits[i*2:i*2+2], 16, 8)
			if err != nil {
				return rgb{}, false
			}
			channels[i] = float64(n) / 255
		}
		return rgb{channels[0], channels[1], channels[2]}, true
	}

	if match := oklchColour.FindStringSubmatch(text); match != nil {
		lightness, err := strconv.ParseFloat(strings.TrimSuffix(match[1], "%"), 64)
		if err != nil {
			return rgb{}, false
		}
		if strings.HasSuffix(match[1], "%") {
			lightness /= 100
		}
		chroma, err1 := strconv.ParseFloat(match[2], 64)
		hue, err2 := strconv.ParseFloat(match[3], 64)
		if err1 != nil || err2 != nil {
			return rgb{}, false
		}
		return oklchToRGB(lightness, chroma, hue), true
	}

	return rgb{}, false
}

// oklchToRGB converts by way of the Oklab and linear-RGB steps the spec defines.
func oklchToRGB(lightness, chroma, hueDegrees float64) rgb {
	hue := hueDegrees * math.Pi / 180
	a := chroma * math.Cos(hue)
	b := chroma * math.Sin(hue)

	// The spec cubes each of these. Written as a multiplication rather than
	// math.Pow: for an integer exponent Pow is both slower and less exact, and
	// this runs for every colour a theme defines.
	lRoot := lightness + 0.3963377774*a + 0.2158037573*b
	mRoot := lightness - 0.1055613458*a - 0.0638541728*b
	sRoot := lightness - 0.0894841775*a - 1.291485548*b

	l := lRoot * lRoot * lRoot
	m := mRoot * mRoot * mRoot
	s := sRoot * sRoot * sRoot

	return rgb{
		r: gamma(4.0767416621*l - 3.3077115913*m + 0.2309699292*s),
		g: gamma(-1.2684380046*l + 2.6097574011*m - 0.3413193965*s),
		b: gamma(-0.0041960863*l - 0.7034186147*m + 1.707614701*s),
	}
}

// gamma converts linear light to the sRGB values a display shows.
func gamma(channel float64) float64 {
	clamped := math.Max(0, math.Min(1, channel))
	if clamped <= 0.0031308 {
		return clamped * 12.92
	}
	return 1.055*math.Pow(clamped, 1/2.4) - 0.055
}

// luminance is relative luminance, as WCAG defines it.
func luminance(colour rgb) float64 {
	linear := func(channel float64) float64 {
		if channel <= 0.04045 {
			return channel / 12.92
		}
		return math.Pow((channel+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(colour.r) + 0.7152*linear(colour.g) + 0.0722*linear(colour.b)
}

// contrastRatio is the ratio between two colours, 1 to 21.
func contrastRatio(one, other rgb) float64 {
	brighter := math.Max(luminance(one), luminance(other))
	darker := math.Min(luminance(one), luminance(other))
	return (brighter + 0.05) / (darker + 0.05)
}
