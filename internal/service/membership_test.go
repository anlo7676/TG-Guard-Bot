package service

import (
	"context"
	"testing"

	"tgguard/internal/domain"
)

func TestBotMembershipIgnoresPrivateChats(t *testing.T) {
	s := &Service{}
	for _, status := range []string{"member", "kicked"} {
		u := domain.MemberUpdate{
			Chat: domain.Chat{ID: 7932006100, Type: "private"},
			New:  domain.Member{Status: status},
		}
		if err := s.BotMembership(context.Background(), u); err != nil {
			t.Fatalf("status=%s: %v", status, err)
		}
	}
}

func TestGroupIgnoresInvalidChatIDs(t *testing.T) {
	s := &Service{}
	for _, chat := range []domain.Chat{
		{ID: 7932006100, Type: "supergroup"},
		{ID: -100, Type: "private"},
	} {
		if err := s.Group(context.Background(), chat); err != nil {
			t.Fatal(err)
		}
	}
}
