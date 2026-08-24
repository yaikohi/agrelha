// Package config loads agrelha's runtime configuration from the environment.
// Non-secret keys come from the agrelha-config ConfigMap; secrets (CODEBERG_*,
// OIDC_CLIENT_*) come from the agrelha-env Secret. See yaya-ops manifests/agrelha-*.
package config

import "os"

type Config struct {
	ListenAddr string
	DBPath     string

	// Auth (Zitadel OIDC)
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string
	AllowedEmail     string

	// Declarative plane (git)
	GitRepoURL      string
	GitBranch       string
	GitUsername     string
	GitToken        string
	GitAuthorName   string
	GitAuthorEmail  string
	ModsPath        string
	AdminsPath      string
	ThunderstoreAPI string

	// Imperative plane (k8s)
	ValheimNamespace  string
	ValheimDeployment string
	ValheimStatusURL  string

	// Observability
	InfluxDBURL         string
	GrafanaDashboardURL string
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func Load() *Config {
	return &Config{
		ListenAddr: env("LISTEN_ADDR", ":8080"),
		DBPath:     env("DB_PATH", "/data/agrelha.db"),

		OIDCIssuer:       env("OIDC_ISSUER", ""),
		OIDCClientID:     env("OIDC_CLIENT_ID", ""),
		OIDCClientSecret: env("OIDC_CLIENT_SECRET", ""),
		OIDCRedirectURL:  env("OIDC_REDIRECT_URL", ""),
		AllowedEmail:     env("ALLOWED_EMAIL", ""),

		GitRepoURL:      env("GIT_REPO_URL", ""),
		GitBranch:       env("GIT_BRANCH", "main"),
		GitUsername:     env("CODEBERG_USERNAME", ""),
		GitToken:        env("CODEBERG_TOKEN", ""),
		GitAuthorName:   env("GIT_AUTHOR_NAME", "agrelha"),
		GitAuthorEmail:  env("GIT_AUTHOR_EMAIL", "agrelha@ykhi.xyz"),
		ModsPath:        env("MODS_PATH", "manifests/valheim-mods.yaml"),
		AdminsPath:      env("ADMINS_PATH", "manifests/valheim-admins.yaml"),
		ThunderstoreAPI: env("THUNDERSTORE_API", "https://thunderstore.io/c/valheim/api/v1"),

		ValheimNamespace:  env("VALHEIM_NAMESPACE", "valheim"),
		ValheimDeployment: env("VALHEIM_DEPLOYMENT", "valheim"),
		ValheimStatusURL:  env("VALHEIM_STATUS_URL", ""),

		InfluxDBURL:         env("INFLUXDB_URL", ""),
		GrafanaDashboardURL: env("GRAFANA_DASHBOARD_URL", ""),
	}
}
