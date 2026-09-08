package rules

import (
	"fmt"
	"testing"
	"tgguard/internal/domain"
)

func BenchmarkLocalRules(b *testing.B) {
	for _, count := range []int{0, 50} {
		b.Run(fmt.Sprintf("regex_%d", count), func(b *testing.B) {
			s := domain.DefaultSettings()
			for i := 0; i < count; i++ {
				s.AdRules = append(s.AdRules, domain.AdRule{Mode: "regex", Pattern: fmt.Sprintf("(?i)优惠%d(代购|返利)[0-9]+", i), Action: "delete", Enabled: true})
			}
			n := Normalize(domain.Message{ID: 1, Chat: domain.Chat{ID: -100}, From: &domain.User{ID: 77}, Text: "请问 Go 如何创建 HTTP 服务"})
			_ = Evaluate(n, s) // Measure the steady state after compilation.
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = Evaluate(n, s)
			}
		})
	}
}
