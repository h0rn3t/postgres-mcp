package main

import (
	"testing"
	"time"
)

func TestMinNonZero(t *testing.T) {
	tests := []struct{ v, max, want int }{
		{0, 50, 50},
		{-1, 50, 50},
		{10, 50, 10},
		{100, 50, 50},
	}
	for _, tt := range tests {
		if got := minNonZero(tt.v, tt.max); got != tt.want {
			t.Fatalf("minNonZero(%d,%d)=%d want=%d", tt.v, tt.max, got, tt.want)
		}
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: Config{
				DatabaseURL: "postgres://user:pass@localhost/db",
				MaxRows:     100,
				QueryTO:     10 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "missing database URL",
			cfg: Config{
				MaxRows: 100,
				QueryTO: 10 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "invalid max rows - zero",
			cfg: Config{
				DatabaseURL: "postgres://user:pass@localhost/db",
				MaxRows:     0,
				QueryTO:     10 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "invalid max rows - too high",
			cfg: Config{
				DatabaseURL: "postgres://user:pass@localhost/db",
				MaxRows:     20000,
				QueryTO:     10 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "invalid query timeout - too low",
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

func TestSanitizeInput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid input",
			input:   "SELECT * FROM users",
			wantErr: false,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "whitespace only",
			input:   "   ",
			wantErr: true,
		},
		{
			name:    "too long input",
			input:   string(make([]byte, maxQueryLength+1)),
			wantErr: true,
		},
		{
			name:    "suspicious pattern - comments",
			input:   "SELECT * FROM users -- DROP TABLE users",
			wantErr: false, // We warn but don't reject
		},
		{
			name:    "suspicious pattern - union",
			input:   "SELECT * FROM users UNION SELECT * FROM passwords",
			wantErr: false, // We warn but don't reject
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizeInput(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("sanitizeInput() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
