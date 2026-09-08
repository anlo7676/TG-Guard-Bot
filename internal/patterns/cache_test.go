package patterns

import (
	"fmt"
	"sync"
	"testing"
)

func TestCacheBoundedAndConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 300; j++ {
				r, e := Compile(fmt.Sprintf("word%d", j))
				if e != nil || !r.MatchString(fmt.Sprintf("word%d", j)) {
					t.Error("invalid matcher")
				}
			}
		}()
	}
	wg.Wait()
	for i := 0; i < 2200; i++ {
		if _, e := Compile(fmt.Sprintf("unique%d", i)); e != nil {
			t.Fatal(e)
		}
	}
	cache.Lock()
	n := len(cache.items)
	cache.Unlock()
	if n > 2048 {
		t.Fatal("cache unbounded", n)
	}
	if _, err := Compile("["); err == nil {
		t.Fatal("invalid pattern accepted")
	}
}
