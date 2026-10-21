package domain

import "strings"

type Host string

const HostCluster Host = "cluster"

func NormalizeHost(s string) Host {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" {
		return HostCluster
	}
	return Host(t)
}

func (h Host) IsCluster() bool { return NormalizeHost(string(h)) == HostCluster }

func (h Host) String() string { return string(NormalizeHost(string(h))) }
