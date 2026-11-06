package domain

import (
	"errors"
	"regexp"
)

var (
	// ErrInvalidMinecraftUsername is returned when a Minecraft username does not match ^[a-zA-Z0-9_]{1,16}$.
	ErrInvalidMinecraftUsername = errors.New("invalid minecraft username: must be 1-16 alphanumeric characters or underscores")
	mcUsernameRegex             = regexp.MustCompile(`^[a-zA-Z0-9_]{1,16}$`)
)

// ValidateMinecraftUsername validates that a Minecraft username strictly matches ^[a-zA-Z0-9_]{1,16}$.
func ValidateMinecraftUsername(username string) error {
	if !mcUsernameRegex.MatchString(username) {
		return ErrInvalidMinecraftUsername
	}
	return nil
}
