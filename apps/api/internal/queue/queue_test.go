package queue

import (
	"testing"
)

func TestParseRedisConnOpt(t *testing.T) {
	tests := []struct {
		name      string
		addr      string
		wantError bool
	}{
		{
			name:      "Raw host:port",
			addr:      "localhost:6379",
			wantError: false,
		},
		{
			name:      "Redis URI with auth",
			addr:      "redis://default:password@host:6379/0",
			wantError: false,
		},
		{
			name:      "Rediss secure URI",
			addr:      "rediss://default:password@host:6379/0",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opt, err := ParseRedisConnOpt(tt.addr)
			if (err != nil) != tt.wantError {
				t.Fatalf("ParseRedisConnOpt(%q) error = %v, wantError %v", tt.addr, err, tt.wantError)
			}
			if opt == nil {
				t.Fatalf("ParseRedisConnOpt(%q) returned nil opt", tt.addr)
			}
		})
	}
}
