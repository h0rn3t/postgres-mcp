package main

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestDatabaseTransactionIsolation(t *testing.T) {
	// Test that even if SQL injection bypasses our guards,
	// the PostgreSQL read-only transaction prevents any modifications
	// This tests the database-level protection as mentioned in the GitHub issue

	db := mustPool(t)
	defer db.Close()
	resetSchema(t, db)

	// Test various write operations that should be rejected by PostgreSQL
	writeAttempts := []struct {
		name string
		sql  string
	}{
		{"insert", "INSERT INTO users (email, first_name, last_name) VALUES ('test@test.com', 'Test', 'User')"},
		{"update", "UPDATE users SET first_name = 'Hacked' WHERE id = 1"},
		{"delete", "DELETE FROM users WHERE id = 1"},
		{"drop_table", "DROP TABLE users"},
		{"create_table", "CREATE TABLE evil (id INT)"},
		{"truncate", "TRUNCATE TABLE users"},
		{"alter_table", "ALTER TABLE users ADD COLUMN evil TEXT"},
	}

	ctx := t.Context()
	for _, attempt := range writeAttempts {
		t.Run(attempt.name, func(t *testing.T) {
			// Create a new connection and transaction for each test
			conn, err := db.Acquire(ctx)
			if err != nil {
				t.Fatalf("Failed to acquire connection: %v", err)
			}
			defer conn.Release()

			tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatalf("Failed to begin read-only transaction: %v", err)
			}
			defer tx.Rollback(ctx)

			_, err = tx.Exec(ctx, attempt.sql)
			if err == nil {
				t.Fatalf("Expected PostgreSQL to reject %s in read-only transaction, but it was allowed", attempt.name)
			}

			// Verify it's specifically a read-only transaction error
			if !strings.Contains(err.Error(), "read-only") && !strings.Contains(err.Error(), "cannot execute") {
				t.Fatalf("Expected read-only transaction error, got: %v", err)
			}

			t.Logf("PostgreSQL correctly rejected %s: %v", attempt.name, err)
		})
	}
}
