package service

import (
	"context"
	"testing"

	"tgguard/internal/domain"
)

func TestParseCommandAddress(t *testing.T) {
	for _, tc := range []struct {
		text     string
		ok       bool
		cmd, arg string
	}{{"/mute@GuardBot 1h", true, "mute", "1h"}, {"/BAN@guardbot", true, "ban", ""}, {"/ban@OtherBot", false, "", ""}, {"text /ban", false, "", ""}, {"/settings {\"ai_enabled\":true}", true, "settings", `{"ai_enabled":true}`}} {
		cmd, arg, ok := ParseCommand(tc.text, "GuardBot")
		if ok != tc.ok || cmd != tc.cmd || arg != tc.arg {
			t.Errorf("%q => %q %q %v", tc.text, cmd, arg, ok)
		}
	}
}

func TestUnknownCommandIsParsedButNotHandled(t *testing.T) {
	command, arg, ok := ParseCommand("/lucky 1GB", "GuardBot")
	if !ok || command != "lucky" || arg != "1GB" {
		t.Fatalf("unexpected parse: %q %q %v", command, arg, ok)
	}
	if SupportedCommand(command) {
		t.Fatal("foreign command treated as supported")
	}
	if !SupportedCommand("BAN") {
		t.Fatal("supported command lookup must be case-insensitive")
	}
}

func TestUnknownCommandStopsBeforePermissionChecks(t *testing.T) {
	s := &Service{}
	for _, chatType := range []string{"private", "supergroup"} {
		message := domain.Message{
			From: &domain.User{ID: 42},
			Chat: domain.Chat{ID: -100, Type: chatType},
		}
		if err := s.Command(context.Background(), 1, message, "lucky", "1GB"); err != nil {
			t.Fatalf("chat_type=%s: %v", chatType, err)
		}
	}
}
