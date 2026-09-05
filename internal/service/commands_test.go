package service

import "testing"

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
