package domain

import (
	"errors"
	"strings"
	"time"
)

type RequestStatus string

const (
	RequestPending  RequestStatus = "pending"
	RequestApproved RequestStatus = "approved"
	RequestDenied   RequestStatus = "denied"
)

var ErrRequestNotPending = errors.New("request is already decided")

type InstanceRequest struct {
	ID        string
	Subject   string
	GameID    GameID
	Name      string
	Instance  Instance
	Mods      ModList
	Status    RequestStatus
	Note      string
	CreatedAt time.Time
	DecidedAt time.Time
	DecidedBy string
	Number    int
}

func (r InstanceRequest) Pending() bool { return r.Status == RequestPending }

func (r InstanceRequest) Label() string {
	name := strings.TrimSpace(r.Name)
	if name == "" {
		name = "(unnamed)"
	}
	return string(r.GameID) + " - " + name
}
