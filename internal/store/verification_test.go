package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func verificationRows(user int64, status string, expires time.Time) *sqlmock.Rows {
	return sqlmock.NewRows(strings.Split(verifyColumns, ",")).AddRow("token", -100, user, "math", "1+2", HashAnswer("token", "3"), status, 0, "kick", expires, 100, false)
}
func TestAnswerRejectsWrongUserExpiredAndReplay(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		user         int64
		expires      time.Time
	}{{"wrong user", "pending", 99, time.Now().Add(time.Minute)}, {"expired", "pending", 42, time.Now().Add(-time.Minute)}, {"replay", "completing", 42, time.Now().Add(time.Minute)}} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, e := sqlmock.New()
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT .* FROM verification_sessions .* FOR UPDATE").WithArgs("token").WillReturnRows(verificationRows(42, tc.status, tc.expires))
			mock.ExpectRollback()
			_, e = (&Store{DB: db}).Answer(context.Background(), "token", tc.user, "3")
			if !errors.Is(e, ErrVerification) {
				t.Fatal(e)
			}
			if e = mock.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestAnswerTransitionsBeforePermissionRestore(t *testing.T) {
	db, mock, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FOR UPDATE").WithArgs("token").WillReturnRows(verificationRows(42, "pending", time.Now().Add(time.Minute)))
	mock.ExpectExec("UPDATE verification_sessions SET status='completing'").WithArgs("token").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	v, e := (&Store{DB: db}).Answer(context.Background(), "token", 42, "3")
	if e != nil || v.Status != "completing" {
		t.Fatal(v, e)
	}
	if e = mock.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
func TestWrongAnswerPersistsAttempt(t *testing.T) {
	db, mock, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FOR UPDATE").WillReturnRows(verificationRows(42, "pending", time.Now().Add(time.Minute)))
	mock.ExpectExec("UPDATE verification_sessions SET attempts=attempts\\+1").WithArgs("token").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if _, e = (&Store{DB: db}).Answer(context.Background(), "token", 42, "wrong"); !errors.Is(e, ErrVerification) {
		t.Fatal(e)
	}
	if e = mock.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
