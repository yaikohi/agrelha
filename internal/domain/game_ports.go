package domain

import "fmt"

const MinecraftGamePort = 25565

func HostPortFor(profile GameProfile, host Host, number, containerPort int) (int, error) {
	if NormalizeHost(string(host)).IsCluster() || number <= 1 {
		return containerPort, nil
	}
	if profile.PortStride <= 0 {
		return 0, fmt.Errorf("game %q declares no PortStride, so instance %d on host %q would publish the same host ports as instance 01", profile.ID, number, host)
	}
	return containerPort + (number-1)*profile.PortStride, nil
}

func WithHostPorts(profile GameProfile, inst Instance, ports []PortSpec) ([]PortSpec, error) {
	out := make([]PortSpec, 0, len(ports))
	for _, p := range ports {
		hp, err := HostPortFor(profile, inst.EffectiveHost(), inst.Number, p.Port)
		if err != nil {
			return nil, err
		}
		p.HostPort = hp
		out = append(out, p)
	}
	return out, nil
}
