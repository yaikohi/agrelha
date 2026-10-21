package domain

import (
	"fmt"
	"strings"
)

const ValheimMinPasswordLen = 5

var ErrValheimPasswordTooShort = fmt.Errorf("valheim requires a password of at least %d characters", ValheimMinPasswordLen)

var ErrValheimPasswordInName = fmt.Errorf("valheim refuses a password that appears in the server or world name")

func (c *ValheimConfig) ValidatePassword(serverName, worldName string) error {
	if c == nil {
		return ErrValheimPasswordTooShort
	}
	pass := strings.TrimSpace(c.Password)
	if len(pass) < ValheimMinPasswordLen {
		return ErrValheimPasswordTooShort
	}
	lower := strings.ToLower(pass)
	for _, name := range []string{serverName, worldName} {
		if name == "" {
			continue
		}
		if strings.Contains(strings.ToLower(name), lower) {
			return ErrValheimPasswordInName
		}
	}
	return nil
}

func (inst Instance) Validate(profile GameProfile) error {
	if _, ok := ProfileFor(profile.ID); !ok {
		return fmt.Errorf("unknown or unregistered game ID %q", profile.ID)
	}
	if profile.ID == GameValheim {
		if err := inst.Valheim.ValidatePassword(inst.Name, inst.Slug); err != nil {
			return err
		}
	}
	return nil
}
