package minecraft

import (
	"reflect"
	"testing"
)

func TestParseMods(t *testing.T) {
	input := `
# Essential core mods
jei
ferrite-core

# Map & utility
journeymap
sophisticatedbackpacks:1.21.1-1.0
`
	expected := []string{
		"jei",
		"ferrite-core",
		"journeymap",
		"sophisticatedbackpacks:1.21.1-1.0",
	}

	res := ParseMods(input)
	if !reflect.DeepEqual(res, expected) {
		t.Fatalf("expected %v, got %v", expected, res)
	}
}

func TestParseUsers(t *testing.T) {
	input := `
# Server admins
ykhi
yaikohi
`
	expected := []string{"ykhi", "yaikohi"}
	res := ParseUsers(input)
	if !reflect.DeepEqual(res, expected) {
		t.Fatalf("expected %v, got %v", expected, res)
	}
}
