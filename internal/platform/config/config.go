// Package config loads agrelha's runtime configuration from the environment.
// Every key has a default, so agrelha starts with nothing set; see
// docs/configuration.md for what each one turns on. Secrets (GIT_TOKEN,
// OIDC_CLIENT_SECRET) are read from the environment like everything else and are
// expected to arrive from a Secret rather than a ConfigMap.
package config

import (
	"os"
	"strconv"
)

type Config struct {
	ListenAddr string
	DBPath     string

	// Auth (Zitadel OIDC)
	OIDCIssuer        string
	OIDCClientID      string
	OIDCClientSecret  string
	OIDCRedirectURL   string
	OIDCPostLogoutURL string
	AllowedEmail      string

	// Declarative plane (git)
	GitRepoURL      string
	GitBranch       string
	GitUsername     string
	GitToken        string
	GitAuthorName   string
	GitAuthorEmail  string
	ModsPath        string
	AdminsPath      string
	ModConfigsPath  string
	ThunderstoreAPI string

	// Imperative plane (k8s)
	ValheimNamespace  string
	ValheimDeployment string
	ValheimStatusURL  string
	ValheimAddress    string
	GameNodeName      string
	BackupsDir        string

	// Minecraft Modded (NeoForge & Fabric)
	MinecraftModsPath     string
	MinecraftAccessPath   string
	MinecraftConfigsPath  string
	MinecraftNamespace    string
	MinecraftDeployment   string
	MinecraftRconAddr     string
	MinecraftRconPassword string
	ModrinthAPI           string

	// Multi-instance and global budget settings
	TotalBudgetGiB   int
	MaxInstances     int
	MaxRunning       int
	MCTotalBudgetGiB int
	MCMaxInstances   int
	MCMaxRunning     int
	MCLBBaseIP       string
	MCInstancesPath  string
	GameNodeSelector string

	// Valheim Multi-Instance settings
	ValheimTotalBudgetGiB int
	ValheimMaxInstances   int
	ValheimMaxRunning     int
	ValheimLBBaseIP       string
	ValheimInstancesPath  string

	// Runtime & Engine (k8s vs docker)
	Runtime       string
	DockerSocket  string
	LocalStateDir string
	ComposeDir    string

	// Provenance (AGPL section 13: operators of a modified agrelha must offer
	// their users the corresponding source).
	SourceURL string

	// Observability
	InfluxDBURL         string
	GrafanaDashboardURL string
	LogLevel            string
	LogFormat           string
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func envIntWithFallback(primary, fallback string, def int) int {
	if v := os.Getenv(primary); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return envInt(fallback, def)
}

func Load() *Config {
	totalBudget := envIntWithFallback("TOTAL_BUDGET_GIB", "MC_TOTAL_BUDGET_GIB", 24)
	maxInstances := envIntWithFallback("MAX_INSTANCES", "MC_MAX_INSTANCES", 4)
	maxRunning := envIntWithFallback("MAX_RUNNING", "MC_MAX_RUNNING", 2)
	valheimTotalBudget := envInt("VALHEIM_TOTAL_BUDGET_GIB", 16)
	valheimMaxInstances := envInt("VALHEIM_MAX_INSTANCES", 4)
	valheimMaxRunning := envInt("VALHEIM_MAX_RUNNING", 2)

	return &Config{
		ListenAddr: env("LISTEN_ADDR", ":8080"),
		DBPath:     env("DB_PATH", "/data/agrelha.db"),

		OIDCIssuer:        env("OIDC_ISSUER", ""),
		OIDCClientID:      env("OIDC_CLIENT_ID", ""),
		OIDCClientSecret:  env("OIDC_CLIENT_SECRET", ""),
		OIDCRedirectURL:   env("OIDC_REDIRECT_URL", ""),
		OIDCPostLogoutURL: env("OIDC_POST_LOGOUT_URL", ""),
		AllowedEmail:      env("ALLOWED_EMAIL", ""),

		GitRepoURL:      env("GIT_REPO_URL", ""),
		GitBranch:       env("GIT_BRANCH", "main"),
		GitUsername:     env("GIT_USERNAME", env("CODEBERG_USERNAME", "")),
		GitToken:        env("GIT_TOKEN", env("CODEBERG_TOKEN", "")),
		GitAuthorName:   env("GIT_AUTHOR_NAME", "agrelha"),
		GitAuthorEmail:  env("GIT_AUTHOR_EMAIL", "agrelha@localhost"),
		ModsPath:        env("MODS_PATH", "manifests/valheim/mods.yaml"),
		AdminsPath:      env("ADMINS_PATH", "manifests/valheim/admins.yaml"),
		ModConfigsPath:  env("MOD_CONFIGS_PATH", "manifests/valheim/configs.yaml"),
		ThunderstoreAPI: env("THUNDERSTORE_API", "https://thunderstore.io/c/valheim/api/v1"),

		ValheimNamespace:  env("VALHEIM_NAMESPACE", "valheim"),
		ValheimDeployment: env("VALHEIM_DEPLOYMENT", "valheim"),
		ValheimStatusURL:  env("VALHEIM_STATUS_URL", ""),
		ValheimAddress:    env("VALHEIM_ADDRESS", ""),
		GameNodeName:      env("GAME_NODE_NAME", ""),
		BackupsDir:        env("BACKUPS_DIR", ""),

		MinecraftModsPath:     env("MINECRAFT_MODS_PATH", "manifests/minecraft-modded/mods.yaml"),
		MinecraftAccessPath:   env("MINECRAFT_ACCESS_PATH", "manifests/minecraft-modded/access.yaml"),
		MinecraftConfigsPath:  env("MINECRAFT_CONFIGS_PATH", "manifests/minecraft-modded/configs.yaml"),
		MinecraftNamespace:    env("MINECRAFT_NAMESPACE", "minecraft-modded"),
		MinecraftDeployment:   env("MINECRAFT_DEPLOYMENT", "minecraft-modded"),
		MinecraftRconAddr:     env("MINECRAFT_RCON_ADDR", "minecraft-modded.minecraft-modded.svc.cluster.local:25575"),
		MinecraftRconPassword: env("MINECRAFT_RCON_PASSWORD", ""),
		ModrinthAPI:           env("MODRINTH_API", "https://api.modrinth.com/v2"),

		TotalBudgetGiB:   totalBudget,
		MaxInstances:     maxInstances,
		MaxRunning:       maxRunning,
		MCTotalBudgetGiB: totalBudget,
		MCMaxInstances:   maxInstances,
		MCMaxRunning:     maxRunning,
		MCLBBaseIP:       env("MC_LB_BASE_IP", ""),
		MCInstancesPath:  env("MC_INSTANCES_PATH", "manifests/minecraft-modded"),
		GameNodeSelector: env("GAME_NODE_SELECTOR", ""),

		ValheimTotalBudgetGiB: valheimTotalBudget,
		ValheimMaxInstances:   valheimMaxInstances,
		ValheimMaxRunning:     valheimMaxRunning,
		ValheimLBBaseIP:       env("VALHEIM_LB_BASE_IP", ""),
		ValheimInstancesPath:  env("VALHEIM_INSTANCES_PATH", "manifests/valheim"),

		Runtime:       env("RUNTIME", "k8s"),
		DockerSocket:  env("DOCKER_SOCKET", "/var/run/docker.sock"),
		LocalStateDir: env("LOCAL_STATE_DIR", ""),
		ComposeDir:    env("COMPOSE_DIR", "compose"),

		SourceURL: env("SOURCE_URL", "https://codeberg.org/ykhi/agrelha"),

		InfluxDBURL:         env("INFLUXDB_URL", ""),
		GrafanaDashboardURL: env("GRAFANA_DASHBOARD_URL", ""),
		LogLevel:            env("LOG_LEVEL", "info"),
		LogFormat:           env("LOG_FORMAT", "text"),
	}
}
