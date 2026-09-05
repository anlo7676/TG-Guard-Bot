package domain

import (
	"testing"
)

func TestInvalidSettings(t *testing.T) {
	s := DefaultSettings()
	if e := s.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*Settings){func(s *Settings) { s.VerificationType = "web" }, func(s *Settings) { s.AIWarnConfidence = .9; s.AIDeleteConfidence = .8 }, func(s *Settings) { s.MuteSeconds = 5 }, func(s *Settings) { s.Rules["unknown"] = RuleSetting{Enabled: true} }, func(s *Settings) { s.ReviewAccess = "everyone" }} {
		s := DefaultSettings()
		change(&s)
		if e := s.Validate(); e == nil {
			t.Fatalf("accepted invalid %+v", s)
		}
	}
}
func TestRestrictedMembership(t *testing.T) {
	if (Member{Status: "restricted"}).Present() {
		t.Fatal("restricted nonmember is not present")
	}
	if !(Member{Status: "restricted", IsMember: true}).Present() {
		t.Fatal("restricted member is present")
	}
}
