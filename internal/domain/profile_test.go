package domain

import "testing"

func TestProfilePolicyValidation(t *testing.T) {
	s := DefaultSettings()
	if s.ProfileGuardEnabled || s.ProfileAvatarMatch {
		t.Fatal("must opt in")
	}
	if err := ApplySettingsPatch(&s, []byte(`{"profile_guard_enabled":true,"profile_avatar_match":true,"profile_blocked_domains":["heiying.co"]}`)); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"", "https://heiying.co", "heiying.co/path", "HEIYING.CO", "heiying..co", "-heiying.co"} {
		s.ProfileBlockedDomains = []string{d}
		if s.Validate() == nil {
			t.Fatalf("accepted %q", d)
		}
	}
}
