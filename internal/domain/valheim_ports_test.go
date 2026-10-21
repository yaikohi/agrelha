package domain

import "testing"

func TestValheimHostPortsDoNotCollideOnOneHost(t *testing.T) {
	seen := map[int]int{}
	for n := 1; n <= 4; n++ {
		g, q, s := ValheimHostPortsFor("fogo", n)
		for _, p := range []int{g, q, s} {
			if prev, ok := seen[p]; ok {
				t.Fatalf("instance %d and %d both claim host port %d; the second world fails to bind and never starts", prev, n, p)
			}
			seen[p] = n
		}
		if q != g+1 {
			t.Errorf("instance %d: query port %d must be game port %d plus one", n, q, g)
		}
	}
}

func TestClusterWorldsKeepTheStandardPorts(t *testing.T) {
	for n := 1; n <= 4; n++ {
		g, q, s := ValheimHostPortsFor("cluster", n)
		if g != 2456 || q != 2457 || s != 9001 {
			t.Errorf("cluster instance %d = %d/%d/%d; each cluster world has its own LoadBalancer IP, so offsetting would break every existing address", n, g, q, s)
		}
	}
}

func TestFirstDockerWorldMatchesTheStandardPorts(t *testing.T) {
	if g, q, s := ValheimHostPortsFor("fogo", 1); g != 2456 || q != 2457 || s != 9001 {
		t.Errorf("instance 01 on a Docker Host = %d/%d/%d, want the defaults so the migrated world keeps its address", g, q, s)
	}
}
