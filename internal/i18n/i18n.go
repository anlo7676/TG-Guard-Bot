package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed *.json
var files embed.FS
var catalogs = load()

func load() map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, lang := range []string{"zh_CN", "en_US"} {
		b, e := files.ReadFile(lang + ".json")
		if e != nil {
			panic(e)
		}
		var m map[string]string
		if e = json.Unmarshal(b, &m); e != nil {
			panic(e)
		}
		out[lang] = m
	}
	return out
}
func Text(lang, key string, args ...any) string {
	m := catalogs[lang]
	if m == nil {
		m = catalogs["zh_CN"]
	}
	s := m[key]
	if s == "" {
		s = catalogs["zh_CN"][key]
	}
	if s == "" {
		s = key
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}
