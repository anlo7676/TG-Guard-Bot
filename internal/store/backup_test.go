package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func backupFixture(mock sqlmock.Sqlmock) DatabaseBackup {
	b := DatabaseBackup{Version: 1, BotID: 42}
	fields := [6]string{"id", "bigint", "NO", "", "", ""}
	h := sha256.New()
	json.NewEncoder(h).Encode(fields)
	for _, name := range backupTables {
		mock.ExpectQuery("SELECT COLUMN_NAME").WithArgs(name).WillReturnRows(sqlmock.NewRows([]string{"name", "type", "nullable", "default", "extra", "collation"}).AddRow(fields[0], fields[1], fields[2], fields[3], fields[4], fields[5]))
		b.Tables = append(b.Tables, BackupTable{Name: name, Columns: []string{"id"}, Schema: hex.EncodeToString(h.Sum(nil))})
	}
	return b
}

func TestRestoreValidatesAllSchemasBeforeWrites(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectBegin()
	b := backupFixture(mock)
	b.Tables[len(b.Tables)-1].Schema = "different"
	mock.ExpectRollback()
	if err := (&Store{DB: db}).RestoreDatabase(context.Background(), b, 42); err == nil {
		t.Fatal("schema mismatch accepted")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRollbackAndParameterizedValues(t *testing.T) {
	for _, fail := range []bool{true, false} {
		db, mock, _ := sqlmock.New()
		mock.ExpectBegin()
		b := backupFixture(mock)
		payload := "'; DROP TABLE users; --"
		b.Tables[0].Rows = [][]*string{{&payload}}
		for i, name := range backupTables {
			mock.ExpectExec(regexp.QuoteMeta("DELETE FROM `" + name + "`")).WillReturnResult(sqlmock.NewResult(0, 1))
			if i == 0 {
				stmt := mock.ExpectPrepare(regexp.QuoteMeta("INSERT INTO `" + name + "` (`id`) VALUES (?)"))
				if fail {
					stmt.ExpectExec().WithArgs(payload).WillReturnError(errors.New("insert failed"))
					break
				}
				stmt.ExpectExec().WithArgs(payload).WillReturnResult(sqlmock.NewResult(0, 1))
			}
		}
		if fail {
			mock.ExpectRollback()
		} else {
			mock.ExpectQuery("SELECT value FROM bot_state").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(42))
			mock.ExpectExec("INSERT INTO bot_state").WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
		}
		err := (&Store{DB: db}).RestoreDatabase(context.Background(), b, 42)
		if (err != nil) != fail {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}

func TestRestoreRejectsUntrustedStructure(t *testing.T) {
	for _, b := range []DatabaseBackup{{Version: 1, BotID: 42}, {Version: 2, BotID: 42}, {Version: 1, BotID: 7}} {
		if err := (&Store{}).RestoreDatabase(context.Background(), b, 42); err == nil {
			t.Fatal("malformed backup accepted")
		}
	}
}
