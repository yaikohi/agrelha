package domain

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Role string

const (
	RoleAdmin Role = "agrelha-admin"
	RoleUser  Role = "agrelha-user"
)

const rolePrefix = "agrelha-"

func InstanceRole(game GameID, number int) Role {
	return Role(fmt.Sprintf("%s%s-%02d", rolePrefix, game, number))
}

func ParseInstanceRole(r Role) (GameID, int, bool) {
	s := string(r)
	if !strings.HasPrefix(s, rolePrefix) {
		return "", 0, false
	}
	rest := s[len(rolePrefix):]
	cut := strings.LastIndex(rest, "-")
	if cut <= 0 {
		return "", 0, false
	}
	game := GameID(rest[:cut])
	if game != GameValheim && game != GameMinecraft {
		return "", 0, false
	}
	number, err := strconv.Atoi(rest[cut+1:])
	if err != nil || number <= 0 {
		return "", 0, false
	}
	if InstanceRole(game, number) != r {
		return "", 0, false
	}
	return game, number, true
}

func IsAgrelhaRole(r Role) bool { return strings.HasPrefix(string(r), rolePrefix) }

type Identity struct {
	Subject string
	Email   string
	Name    string
	Roles   []Role
}

func (i Identity) HasRole(want Role) bool {
	return slices.Contains(i.Roles, want)
}

func (i Identity) MaySignIn() bool {
	return slices.ContainsFunc(i.Roles, IsAgrelhaRole)
}

func (i Identity) InstanceRoles() []Role {
	var out []Role
	for _, r := range i.Roles {
		if _, _, ok := ParseInstanceRole(r); ok {
			out = append(out, r)
		}
	}
	return out
}

type Account struct {
	Subject   string
	Email     string
	Name      string
	FirstSeen time.Time
	LastSeen  time.Time
}

func (a Account) Label() string {
	switch {
	case a.Email != "":
		return a.Email
	case a.Name != "":
		return a.Name
	default:
		return a.Subject
	}
}

type Principal struct {
	Identity
	Admin bool
}

func NewPrincipal(id Identity) Principal {
	return Principal{Identity: id, Admin: id.HasRole(RoleAdmin)}
}

func (p Principal) SignedIn() bool { return p.Subject != "" }

func (p Principal) CanOperate(game GameID, number int) bool {
	if !p.SignedIn() {
		return false
	}
	if p.Admin {
		return true
	}
	return p.HasRole(InstanceRole(game, number))
}

func (p Principal) CanSee(game GameID, number int) bool { return p.CanOperate(game, number) }
