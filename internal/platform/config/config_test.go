package config

import (
	"os"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	// Temporarily clear relevant env vars to test defaults
	keys := []string{
		"LISTEN_ADDR", "DB_PATH", "GIT_BRANCH", "GIT_USERNAME", "CODEBERG_USERNAME",
		"GIT_TOKEN", "CODEBERG_TOKEN", "GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL",
		"TOTAL_BUDGET_GIB", "MAX_INSTANCES", "MAX_RUNNING",
		"MC_TOTAL_BUDGET_GIB", "MC_MAX_INSTANCES", "MC_MAX_RUNNING",
		"MC_LB_BASE_IP", "GAME_NODE_SELECTOR", "LOG_LEVEL", "LOG_FORMAT",
	}
	for _, k := range keys {
		t.Setenv(k, "")
	}

	cfg := Load()

	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want :8080", cfg.ListenAddr)
	}
	if cfg.TotalBudgetGiB != 24 {
		t.Errorf("TotalBudgetGiB = %d, want 24", cfg.TotalBudgetGiB)
	}
	if cfg.DBPath != "/data/agrelha.db" {
		t.Errorf("DBPath = %q, want /data/agrelha.db", cfg.DBPath)
	}
	if cfg.GitBranch != "main" {
		t.Errorf("GitBranch = %q, want main", cfg.GitBranch)
	}
	if cfg.GitAuthorName != "agrelha" {
		t.Errorf("GitAuthorName = %q, want agrelha", cfg.GitAuthorName)
	}
	if cfg.GitAuthorEmail != "agrelha@localhost" {
		t.Errorf("GitAuthorEmail = %q, want agrelha@localhost", cfg.GitAuthorEmail)
	}
	if cfg.MCTotalBudgetGiB != 24 {
		t.Errorf("MCTotalBudgetGiB = %d, want 24", cfg.MCTotalBudgetGiB)
	}
	if cfg.MCMaxInstances != 4 {
		t.Errorf("MCMaxInstances = %d, want 4", cfg.MCMaxInstances)
	}
	if cfg.MCMaxRunning != 2 {
		t.Errorf("MCMaxRunning = %d, want 2", cfg.MCMaxRunning)
	}
	// Empty means "unconstrained", letting cluster / loadbalancer choose
	if cfg.MCLBBaseIP != "" {
		t.Errorf("MCLBBaseIP default must be empty (unconstrained), got %q", cfg.MCLBBaseIP)
	}
	if cfg.GameNodeSelector != "" {
		t.Errorf("GameNodeSelector default must be empty (schedule anywhere), got %q", cfg.GameNodeSelector)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.LogLevel)
	}
	if cfg.LogFormat != "text" {
		t.Errorf("LogFormat = %q, want text", cfg.LogFormat)
	}
	if cfg.Runtime != "k8s" {
		t.Errorf("Runtime = %q, want k8s", cfg.Runtime)
	}
	if cfg.DockerSocket != "/var/run/docker.sock" {
		t.Errorf("DockerSocket = %q, want /var/run/docker.sock", cfg.DockerSocket)
	}
}

func TestGitCredentialsFallbackAndPrecedence(t *testing.T) {
	t.Run("fallback_to_codeberg_env", func(t *testing.T) {
		t.Setenv("GIT_USERNAME", "")
		t.Setenv("GIT_TOKEN", "")
		t.Setenv("CODEBERG_USERNAME", "cb-user")
		t.Setenv("CODEBERG_TOKEN", "cb-token")

		cfg := Load()
		if cfg.GitUsername != "cb-user" {
			t.Errorf("GitUsername = %q, want cb-user (fallback)", cfg.GitUsername)
		}
		if cfg.GitToken != "cb-token" {
			t.Errorf("GitToken = %q, want cb-token (fallback)", cfg.GitToken)
		}
	})

	t.Run("git_env_takes_precedence_over_codeberg", func(t *testing.T) {
		t.Setenv("GIT_USERNAME", "git-user")
		t.Setenv("GIT_TOKEN", "git-token")
		t.Setenv("CODEBERG_USERNAME", "cb-user")
		t.Setenv("CODEBERG_TOKEN", "cb-token")

		cfg := Load()
		if cfg.GitUsername != "git-user" {
			t.Errorf("GitUsername = %q, want git-user (precedence)", cfg.GitUsername)
		}
		if cfg.GitToken != "git-token" {
			t.Errorf("GitToken = %q, want git-token (precedence)", cfg.GitToken)
		}
	})
}

func TestEnvHelpers(t *testing.T) {
	t.Run("env_helper", func(t *testing.T) {
		t.Setenv("TEST_KEY_EXISTS", "hello")
		if got := env("TEST_KEY_EXISTS", "default"); got != "hello" {
			t.Errorf("env = %q, want hello", got)
		}
		if got := env("TEST_KEY_NONEXISTENT", "default"); got != "default" {
			t.Errorf("env = %q, want default", got)
		}
	})

	t.Run("envInt_helper", func(t *testing.T) {
		t.Setenv("TEST_INT_VALID", "42")
		t.Setenv("TEST_INT_INVALID", "not-a-number")
		_ = os.Unsetenv("TEST_INT_UNSET")

		if got := envInt("TEST_INT_VALID", 10); got != 42 {
			t.Errorf("envInt valid = %d, want 42", got)
		}
		if got := envInt("TEST_INT_INVALID", 10); got != 10 {
			t.Errorf("envInt invalid = %d, want fallback 10", got)
		}
		if got := envInt("TEST_INT_UNSET", 10); got != 10 {
			t.Errorf("envInt unset = %d, want fallback 10", got)
		}
	})
}

func TestBudgetEnvFallbackAndPrecedence(t *testing.T) {
	t.Run("fallback_to_mc_env", func(t *testing.T) {
		t.Setenv("TOTAL_BUDGET_GIB", "")
		t.Setenv("MAX_INSTANCES", "")
		t.Setenv("MAX_RUNNING", "")
		t.Setenv("MC_TOTAL_BUDGET_GIB", "32")
		t.Setenv("MC_MAX_INSTANCES", "6")
		t.Setenv("MC_MAX_RUNNING", "3")

		cfg := Load()
		if cfg.TotalBudgetGiB != 32 || cfg.MCTotalBudgetGiB != 32 {
			t.Errorf("TotalBudgetGiB = %d, want 32", cfg.TotalBudgetGiB)
		}
		if cfg.MaxInstances != 6 || cfg.MCMaxInstances != 6 {
			t.Errorf("MaxInstances = %d, want 6", cfg.MaxInstances)
		}
		if cfg.MaxRunning != 3 || cfg.MCMaxRunning != 3 {
			t.Errorf("MaxRunning = %d, want 3", cfg.MaxRunning)
		}
	})

	t.Run("global_env_takes_precedence", func(t *testing.T) {
		t.Setenv("TOTAL_BUDGET_GIB", "48")
		t.Setenv("MAX_INSTANCES", "8")
		t.Setenv("MAX_RUNNING", "4")
		t.Setenv("MC_TOTAL_BUDGET_GIB", "32")
		t.Setenv("MC_MAX_INSTANCES", "6")
		t.Setenv("MC_MAX_RUNNING", "3")

		cfg := Load()
		if cfg.TotalBudgetGiB != 48 {
			t.Errorf("TotalBudgetGiB = %d, want 48 (precedence)", cfg.TotalBudgetGiB)
		}
		if cfg.MaxInstances != 8 {
			t.Errorf("MaxInstances = %d, want 8 (precedence)", cfg.MaxInstances)
		}
		if cfg.MaxRunning != 4 {
			t.Errorf("MaxRunning = %d, want 4 (precedence)", cfg.MaxRunning)
		}
	})
}
