package api

import (
	"strconv"
	"strings"
	"sync"
)

/*
BannerWords are what the desktop's banners say around the names in them, in
the language the person reads the interface in.

The webview has the translations and Go raises the banners, so the webview
hands the words over. Each is a template: {name}, {room} and {count} are
filled in here. Anything left empty keeps the English it replaces, which is
also what a banner says before the webview has spoken at all.
*/
type BannerWords struct {
	Calling         string `json:"calling"`
	DirectCall      string `json:"directCall"`
	Conference      string `json:"conference"`
	InChannel       string `json:"inChannel"`
	StartingSoon    string `json:"startingSoon"`
	StartsTomorrow  string `json:"startsTomorrow"`
	StartsInHour    string `json:"startsInHour"`
	StartsInHours   string `json:"startsInHours"`
	StartsInMinute  string `json:"startsInMinute"`
	StartsInMinutes string `json:"startsInMinutes"`
}

var englishBanners = BannerWords{
	Calling:         "{name} is calling",
	DirectCall:      "A direct call",
	Conference:      "Invites you to join a conference call",
	InChannel:       "In #{room}",
	StartingSoon:    "Starting soon",
	StartsTomorrow:  "Starts tomorrow",
	StartsInHour:    "Starts in an hour",
	StartsInHours:   "Starts in {count} hours",
	StartsInMinute:  "Starts in a minute",
	StartsInMinutes: "Starts in {count} minutes",
}

type bannerWords struct {
	mu    sync.RWMutex
	words BannerWords
}

// UseBannerWords takes the banners' words in the interface's language.
func (s *Service) UseBannerWords(words BannerWords) {
	s.words.mu.Lock()
	defer s.words.mu.Unlock()
	s.words.words = words
}

// banner fills in one template, falling back to English where none was given.
func (s *Service) banner(pick func(BannerWords) string, values map[string]string) string {
	s.words.mu.RLock()
	template := pick(s.words.words)
	s.words.mu.RUnlock()
	if strings.TrimSpace(template) == "" {
		template = pick(englishBanners)
	}
	for key, value := range values {
		template = strings.ReplaceAll(template, "{"+key+"}", value)
	}
	return template
}

// startsIn is a reminder's body, from the minutes left until the event.
func (s *Service) startsIn(minutes int) string {
	switch {
	case minutes >= 1440:
		return s.banner(func(w BannerWords) string { return w.StartsTomorrow }, nil)
	case minutes >= 120:
		return s.banner(func(w BannerWords) string { return w.StartsInHours },
			map[string]string{"count": strconv.Itoa(minutes / 60)})
	case minutes >= 60:
		return s.banner(func(w BannerWords) string { return w.StartsInHour }, nil)
	case minutes > 1:
		return s.banner(func(w BannerWords) string { return w.StartsInMinutes },
			map[string]string{"count": strconv.Itoa(minutes)})
	case minutes == 1:
		return s.banner(func(w BannerWords) string { return w.StartsInMinute }, nil)
	}
	return s.banner(func(w BannerWords) string { return w.StartingSoon }, nil)
}
