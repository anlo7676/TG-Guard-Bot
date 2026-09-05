package bot

import "testing"

func TestMentionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		s    string
		want bool
	}{{"@GuardBot 是广告吗", true}, {"@guardbot", true}, {"@GuardBotOther", false}, {"@guardbot_foo", false}, {"hello", false}, {"@guardbotother @guardbot", true}} {
		if MentionsBot(tc.s, "GuardBot") != tc.want {
			t.Error(tc.s)
		}
	}
}
