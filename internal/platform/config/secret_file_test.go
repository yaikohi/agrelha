package config

import (
	"os"
	"path/filepath"
	"testing"
)

const mangledByEnvironmentFile = `{"type":"serviceaccount","keyId":"1","key":"-----BEGIN RSA PRIVATE KEY-----nMIIEow-----END RSA PRIVATE KEY-----n","userId":"2"}`

func TestASecretFileCarriesNewlinesThatAnEnvironmentFileDestroys(t *testing.T) {
	const withNewlines = "{\"key\":\"-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----\n\"}"

	path := filepath.Join(t.TempDir(), "key.json")
	if err := os.WriteFile(path, []byte(withNewlines), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_KEY_FILE", path)
	t.Setenv("TEST_KEY", mangledByEnvironmentFile)

	got := envSecret("TEST_KEY", "")
	if got != withNewlines {
		t.Errorf(`envSecret did not return the file's bytes verbatim.

A systemd EnvironmentFile consumes the backslash of every \n escape, so a PEM arriving that way
has no newlines left and pem.Decode refuses it - while the JSON still parses, which is why the
failure surfaces far from its cause. Reading the path keeps the bytes intact.
got  %q
want %q`, got, withNewlines)
	}
}

func TestTheEnvVarStillWorksWhenNoFileIsNamed(t *testing.T) {
	t.Setenv("TEST_KEY", "plain-value")
	if got := envSecret("TEST_KEY", ""); got != "plain-value" {
		t.Errorf("envSecret ignored the plain environment variable; got %q", got)
	}
	if got := envSecret("TEST_ABSENT_KEY", "fallback"); got != "fallback" {
		t.Errorf("envSecret lost the default; got %q", got)
	}
}

func TestAnUnreadableSecretFileDoesNotSilentlyFallBackToTheEnvVar(t *testing.T) {
	t.Setenv("TEST_KEY_FILE", filepath.Join(t.TempDir(), "does-not-exist.json"))
	t.Setenv("TEST_KEY", mangledByEnvironmentFile)

	if got := envSecret("TEST_KEY", ""); got != "" {
		t.Errorf(`envSecret fell back to the environment variable when the named file could not be read.

The variable is the mangled copy this option exists to avoid, so falling back would reintroduce
exactly the bug - and silently. An unreadable secret file must disable the feature loudly.
got %q`, got)
	}
}

func TestTrailingNewlineIsTrimmedButInnerNewlinesSurvive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key.json")
	if err := os.WriteFile(path, []byte("a\nb\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_KEY_FILE", path)
	if got := envSecret("TEST_KEY", ""); got != "a\nb" {
		t.Errorf("want %q, got %q", "a\nb", got)
	}
}
