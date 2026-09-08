// Package patterns shares a bounded cache between configuration validation and matching.
package patterns

import (
	"regexp"
	"sync"
)

var cache = struct {
	sync.RWMutex
	items map[string]*regexp.Regexp
}{items: make(map[string]*regexp.Regexp)}

func Compile(pattern string) (*regexp.Regexp, error) {
	cache.RLock()
	r := cache.items[pattern]
	cache.RUnlock()
	if r != nil {
		return r, nil
	}
	cache.Lock()
	defer cache.Unlock()
	if r := cache.items[pattern]; r != nil {
		return r, nil
	}
	r, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	if len(cache.items) >= 2048 {
		clear(cache.items)
	}
	cache.items[pattern] = r
	return r, nil
}
