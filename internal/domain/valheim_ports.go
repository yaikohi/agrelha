package domain

const (
	ValheimGamePort       = 2456
	ValheimQueryPort      = 2457
	ValheimStatusPort     = 9001
	valheimPortsPerWorld  = 2
	valheimStatusPortBase = 9000
)

func ValheimHostPorts(inst Instance) (game, query, status int) {
	return ValheimHostPortsFor(inst.EffectiveHost(), inst.Number)
}

func ValheimHostPortsFor(host Host, number int) (game, query, status int) {
	if NormalizeHost(string(host)).IsCluster() || number < 1 {
		return ValheimGamePort, ValheimQueryPort, ValheimStatusPort
	}
	game = ValheimGamePort + (number-1)*valheimPortsPerWorld
	return game, game + 1, valheimStatusPortBase + number
}
