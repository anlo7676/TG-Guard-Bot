package store

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"tgguard/internal/domain"
)

func TestInvalidChatsCannotReachGroupStorage(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Store{DB: db}
	if err = s.RegisterGroup(context.Background(), domain.Chat{ID: 7932006100, Type: "private"}); err != nil {
		t.Fatal(err)
	}
	if err = s.RegisterGroup(context.Background(), domain.Chat{ID: 7932006100, Type: "supergroup"}); err != nil {
		t.Fatal(err)
	}
	if err = s.DeactivateGroup(context.Background(), 7932006100); err != nil {
		t.Fatal(err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
