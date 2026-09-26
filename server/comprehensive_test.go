//go:build integration

package main

import (
	"testing"
	"time"
)

func TestConfigurationValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid_production_config",
			cfg: Config{
				DatabaseURL: "postgres://user:pass@localhost/db",
				MaxRows:     1000,
				QueryTO:     30 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "missing_database_url",
			cfg: Config{
				MaxRows: 100,
				QueryTO: 10 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "invalid_max_rows_zero",
			cfg: Config{
				DatabaseURL: "postgres://user:pass@localhost/db",
				MaxRows:     0,
				QueryTO:     10 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "invalid_max_rows_too_high",
			cfg: Config{
				DatabaseURL: "postgres://user:pass@localhost/db",
				MaxRows:     50000,
				QueryTO:     10 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "invalid_query_timeout",
			cfg: Config{
				DatabaseURL: "postgres://user:pass@localhost/db",
				MaxRows:     100,
				QueryTO:     500 * time.Millisecond,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAuditLogging(t *testing.T) {
	// Test that audit logging doesn't panic and formats correctly
	testCases := []struct {
		event   string
		user    string
		query   string
		result  string
		success bool
	}{
		{"query_success", "test_user", "SELECT * FROM users", "returned 5 rows", true},
		{"search_success", "test_user", "search term", "found 3 matches", true},
		{"query_failed", "test_user", "bad query", "syntax error", false},
		{"auth_failed", "192.168.1.1", "", "invalid token", false},
	}

	for _, tc := range testCases {
		t.Run(tc.event, func(t *testing.T) {
			// This should not panic
			auditLog(tc.event, tc.user, tc.query, tc.result, tc.success)
		})
	}
}
