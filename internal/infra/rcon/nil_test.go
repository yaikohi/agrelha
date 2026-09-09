package rcon

import (
	"errors"
	"testing"
)

func TestNilClientDoesNotPanic(t *testing.T) {
	var c *Client
	if _, err := c.Execute("/list"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Execute on nil client: got %v, want ErrNotConfigured", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close on nil client: %v", err)
	}
}
