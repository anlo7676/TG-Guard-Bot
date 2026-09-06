package rules

import (
	"regexp"
	"tgguard/internal/domain"
)

type adPreset struct {
	rule  domain.BuiltinRule
	regex *regexp.Regexp
}

var compiledAdPresets = func() []adPreset {
	out := []adPreset{}
	for _, r := range domain.BuiltinRules {
		if r.Pattern != "" {
			out = append(out, adPreset{r, regexp.MustCompile("(?i)" + r.Pattern)})
		}
	}
	return out
}()
