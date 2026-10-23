package settings

import (
	"fmt"
	"strings"
)

/*
Message translation.

On by default, because switching it on sends nothing anywhere by itself:
translating on the reader's own device sends nothing, the operator's
LibreTranslate instance exists only if the operator configured one, and every
other provider needs a member to connect it with their own key. An operator
who wants none of it switches it off; one who wants members' messages kept away
from commercial providers removes those from translation.providers.
*/
const FeatureTranslation Feature = "translation"

const (
	/*
		TranslationProviders are the providers members may use,
		comma-separated: device, libretranslate, google, microsoft, deepl.
		All of them by default.
	*/
	TranslationProviders Key = "translation.providers"

	/*
		TranslationRequestsPerHour bounds how often one person may send
		messages to a provider through this server. A request is up to fifty
		messages; translations already made are served without counting, so
		this is a ceiling on cost and on a member using this server as a free
		translation proxy, not on reading.
	*/
	TranslationRequestsPerHour Key = "translation.requests_per_hour"
)

// translationProviderNames is every provider a member may be allowed.
var translationProviderNames = []string{"device", "libretranslate", "google", "microsoft", "deepl"}

func init() {
	defineFeature(FeatureTranslation, true,
		"Translating messages inline. Sends nothing anywhere by itself: members translate on their own device, through the operator's LibreTranslate instance if one is configured, or through a provider they connect with their own key.")

	define(map[Key]definition{
		TranslationProviders: {
			fallback:    strings.Join(translationProviderNames, ","),
			description: "Translation providers members may use, comma-separated: device, libretranslate, google, microsoft, deepl. Google, Microsoft and DeepL see the text of every message translated with them.",
			validate:    translationProviders,
		},
		TranslationRequestsPerHour: {
			fallback:    "120",
			description: "How many translation requests one person may send to a provider in an hour. Translations already made do not count.",
			validate:    atLeastOne,
		},
	})
}

func translationProviders(value string) error {
	for _, name := range ListOf(value) {
		known := false
		for _, provider := range translationProviderNames {
			known = known || name == provider
		}
		if !known {
			return fmt.Errorf("%w: %q is not a translation provider; use %s",
				ErrInvalidValue, name, strings.Join(translationProviderNames, ", "))
		}
	}
	return nil
}
