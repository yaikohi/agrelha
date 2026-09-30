package domain

import "regexp"

type ConnEvent int

const (
	ConnNone ConnEvent = iota
	ConnJoined
	ConnLeft
)

var (
	valheimConnectRe    = regexp.MustCompile(`Got connection SteamID (\d{17})`)
	valheimDisconnectRe = regexp.MustCompile(`Closing socket (\d{17})`)
)

func ValheimConnection(line string) (string, ConnEvent) {
	if m := valheimConnectRe.FindStringSubmatch(line); m != nil {
		return m[1], ConnJoined
	}
	if m := valheimDisconnectRe.FindStringSubmatch(line); m != nil {
		return m[1], ConnLeft
	}
	return "", ConnNone
}
