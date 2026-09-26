package main

import (
	"strings"
	"testing"
)

func TestSanitizeInputComprehensive(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		wantWarn bool // Should trigger warning log
	}{
		// Valid inputs
		{
			name:    "normal_query",
			input:   "SELECT * FROM users WHERE id = 1",
			wantErr: false,
		},
		{
			name:    "natural_language",
			input:   "Show me all customers from New York",
			wantErr: false,
		},
		{
			name:    "with_numbers",
			input:   "Find orders over $100",
			wantErr: false,
		},

		// Invalid inputs
		{
			name:    "empty_string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "only_whitespace",
			input:   "   \t\n  ",
			wantErr: true,
		},
		{
			name:    "too_long",
			input:   strings.Repeat("SELECT * FROM table ", 1000), // > maxQueryLength
			wantErr: true,
		},

		// Suspicious patterns (warn but don't reject)
		{
			name:     "sql_comment",
			input:    "SELECT * FROM users -- DROP TABLE users",
			wantErr:  false,
			wantWarn: true,
		},
		{
			name:     "block_comment",
			input:    "SELECT * FROM users /* comment */",
			wantErr:  false,
			wantWarn: true,
		},
		{
			name:     "union_query",
			input:    "SELECT * FROM users UNION SELECT * FROM admins",
			wantErr:  false,
			wantWarn: true,
		},
		{
			name:     "system_procedure",
			input:    "EXEC xp_cmdshell 'dir'",
			wantErr:  false,
			wantWarn: true,
		},
		{
			name:     "information_schema",
			input:    "SELECT * FROM information_schema.tables",
			wantErr:  false,
			wantWarn: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizeInput(tt.input)
			hasErr := err != nil

			if hasErr != tt.wantErr {
				t.Fatalf("sanitizeInput(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}

			// Note: We can't easily test warning logs in unit tests without capturing log output
			// In a real implementation, you might use a test logger
		})
	}
}

func TestMinNonZeroEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		v    int
		max  int
		want int
	}{
		{"zero_zero", 0, 0, 0},
		{"negative_zero", -1, 0, 0},
		{"positive_zero", 5, 0, 0},
		{"zero_positive", 0, 10, 10},
		{"equal_values", 5, 5, 5},
		{"large_numbers", 1000000, 999999, 999999},
		{"max_int", 2147483647, 2147483646, 2147483646},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := minNonZero(tt.v, tt.max)
			if got != tt.want {
				t.Fatalf("minNonZero(%d, %d) = %d, want %d", tt.v, tt.max, got, tt.want)
			}
		})
	}
}
