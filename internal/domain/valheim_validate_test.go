package domain

import (
	"errors"
	"testing"
)

func TestValheimRefusesAPasswordItCannotStartWith(t *testing.T) {
	cases := []struct {
		name    string
		pass    string
		world   string
		server  string
		wantErr error
	}{
		{"empty", "", "Odin", "odin", ErrValheimPasswordTooShort},
		{"four characters", "abcd", "Odin", "odin", ErrValheimPasswordTooShort},
		{"whitespace only", "      ", "Odin", "odin", ErrValheimPasswordTooShort},
		{"contained in the server name", "secret", "my-secret-world", "", ErrValheimPasswordInName},
		{"contained in the world name", "secret", "", "a-secret-slug", ErrValheimPasswordInName},
		{"valid", "hunter2x", "Odin", "odin", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := (&ValheimConfig{Password: tc.pass}).ValidatePassword(tc.world, tc.server)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestAMissingValheimConfigIsRefused(t *testing.T) {
	var cfg *ValheimConfig
	if err := cfg.ValidatePassword("Odin", "odin"); err == nil {
		t.Error("a Valheim world with no config has no password, so it cannot start and must not be accepted")
	}
}

func TestOnlyValheimIsPasswordChecked(t *testing.T) {
	gmod := Instance{GameID: GameGMod, Number: 1, Name: "Lareira", Slug: "lareira"}
	if err := gmod.Validate(GModProfile); err != nil {
		t.Errorf("Garry's Mod runs fine with no password: %v", err)
	}

	mc := Instance{GameID: GameMinecraft, Number: 1, Name: "Flux", Slug: "flux"}
	if err := mc.Validate(MinecraftProfile); err != nil {
		t.Errorf("Minecraft has no server password: %v", err)
	}
}
