package domain

import (
	"errors"
	"testing"
)

func TestValidateMinecraftUsername(t *testing.T) {
	tests := []struct {
		name     string
		username string
		wantErr  bool
	}{
		// Valid names
		{name: "valid Steve", username: "Steve", wantErr: false},
		{name: "valid Alex_123", username: "Alex_123", wantErr: false},
		{name: "valid Notch", username: "Notch", wantErr: false},
		{name: "valid single char", username: "a", wantErr: false},
		{name: "valid 16 chars", username: "1234567890123456", wantErr: false},

		// Invalid names
		{name: "empty username", username: "", wantErr: true},
		{name: "newline injection", username: "steve\n/op malicious", wantErr: true},
		{name: "semicolon / commands", username: "player;kick all", wantErr: true},
		{name: "too long (>16 chars)", username: "player_with_a_name_that_is_far_too_long", wantErr: true},
		{name: "spaces", username: "user name", wantErr: true},
		{name: "leading space", username: " Steve", wantErr: true},
		{name: "trailing space", username: "Steve ", wantErr: true},
		{name: "symbols", username: "steve!", wantErr: true},
		{name: "dashes", username: "steve-alex", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMinecraftUsername(tt.username)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateMinecraftUsername(%q) error = %v, wantErr %v", tt.username, err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, ErrInvalidMinecraftUsername) {
				t.Fatalf("ValidateMinecraftUsername(%q) error = %v, want %v", tt.username, err, ErrInvalidMinecraftUsername)
			}
		})
	}
}
