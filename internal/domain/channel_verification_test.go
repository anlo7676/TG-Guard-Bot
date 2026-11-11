package domain

import "testing"

func TestChannelVerificationSettings(t *testing.T) {
	s := DefaultSettings()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	s.VerificationChannelEnabled = true
	if err := s.Validate(); err == nil {
		t.Fatal("missing target accepted")
	}
	s.VerificationChannelID = -1001234567890
	for _, link := range []string{"https://t.me/example_channel", "https://t.me/+Abc-123_xyz", "https://t.me/joinchat/Abc123"} {
		s.VerificationChannelURL = link
		if err := s.Validate(); err != nil {
			t.Fatal(link, err)
		}
	}
	for _, link := range []string{"https://evil.test/a", "https://t.me/", "https://t.me/user/123", "http://t.me/channel", "https://t.me/channel?start=payload", "https://t.me@evil.test/channel"} {
		s.VerificationChannelURL = link
		if err := s.Validate(); err == nil {
			t.Fatal("invalid link accepted", link)
		}
	}
}
