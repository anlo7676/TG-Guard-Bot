package buildinfo

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"time"
)

var Version = "1.2.0"
var BuiltAt = "unknown"
var Source = "development"
var StartedAt = time.Now().UTC()

func Info() map[string]any {
	revision, dirty := "unknown", false
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, v := range info.Settings {
			switch v.Key {
			case "vcs.revision":
				revision = v.Value
			case "vcs.modified":
				dirty = v.Value == "true"
			}
		}
	}
	return map[string]any{"version": Version, "source": Source, "built_at": BuiltAt, "revision": revision, "modified": dirty, "go": runtime.Version(), "started_at": StartedAt}
}
func Label() string { return fmt.Sprintf("TG Guard %s · %s", Version, Source) }
