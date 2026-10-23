package autoreply

import (
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

func TestApplies(t *testing.T) {
	now := time.Now()
	later, earlier := now.Add(time.Hour), now.Add(-time.Hour)
	busy := dbgen.AutomaticReply{Enabled: true, Statuses: []string{"busy"}}
	anyStatus := dbgen.AutomaticReply{Enabled: true}

	cases := []struct {
		name      string
		rule      dbgen.AutomaticReply
		status    string
		invisible bool
		want      bool
	}{
		{"off", dbgen.AutomaticReply{}, "busy", false, false},
		{"its status", busy, "busy", false, true},
		{"another status", busy, "away", false, false},
		{"its status while appearing offline", busy, "busy", true, false},
		{"tied to appear offline", dbgen.AutomaticReply{Enabled: true, Statuses: []string{AppearOffline}}, "busy", true, true},
		{"no status, appearing offline", anyStatus, "available", true, true},
		{"not started", dbgen.AutomaticReply{Enabled: true, StartsAt: &later}, "available", false, false},
		{"ended", dbgen.AutomaticReply{Enabled: true, EndsAt: &earlier}, "available", false, false},
		{"inside", dbgen.AutomaticReply{Enabled: true, StartsAt: &earlier, EndsAt: &later}, "available", false, true},
	}
	for _, c := range cases {
		if got := Applies(c.rule, c.status, c.invisible, now); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTextFallsBackForMentions(t *testing.T) {
	rule := dbgen.AutomaticReply{Message: "direct"}
	if Text(rule, Mention) != "direct" {
		t.Error("an empty mention text did not fall back")
	}
	rule.MentionMessage = "mention"
	if Text(rule, Mention) != "mention" || Text(rule, Direct) != "direct" {
		t.Error("the texts were mixed up")
	}
}
