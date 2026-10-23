package config

import "testing"

func TestAGameCanOverrideTheDefaultHost(t *testing.T) {
	t.Setenv("DEFAULT_HOST_MINECRAFT", "cluster")
	byGame := defaultHostByGame()
	if byGame["minecraft"] != "cluster" {
		t.Errorf(`DEFAULT_HOST_MINECRAFT did not reach the per-game map; got %v.

DEFAULT_HOST is global, so one setting decides where every game lands. A game whose Host has no
renderer then cannot be created at all, with no way to send it somewhere that works.`, byGame)
	}
	if _, ok := byGame["valheim"]; ok {
		t.Errorf("an unset game must not appear in the map, or it would shadow DEFAULT_HOST with an empty Host; got %v", byGame)
	}
}
