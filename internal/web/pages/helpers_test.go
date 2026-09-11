package pages

import "testing"

func TestConnectAddressShowsPendingHonestly(t *testing.T) {
	if got := ConnectAddress("192.168.20.241", 25565); got != "192.168.20.241:25565" {
		t.Errorf("got %q", got)
	}
	if got := ConnectAddress("", 25565); got == ":25565" || got == "25565" {
		t.Errorf("an unallocated LoadBalancer must not render as an address: %q", got)
	}
	if got := ConnectAddress("   ", 2456); got != "awaiting address" {
		t.Errorf("blank address = %q, want a pending message", got)
	}
}
