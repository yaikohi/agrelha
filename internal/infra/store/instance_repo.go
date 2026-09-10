package store

import (
	"agrelha/internal/domain"
)

// InstanceRepo implements ports.InstanceRepository. It owns the record shape
// and the conversion to and from domain.Instance, so callers never see
// InstanceRecord — that type is a storage detail, not a domain concept.
type InstanceRepo struct {
	s      *Store
	gameID domain.GameID
}

func NewInstanceRepo(s *Store) *InstanceRepo {
	return &InstanceRepo{s: s, gameID: domain.GameMinecraft}
}

func NewValheimInstanceRepo(s *Store) *InstanceRepo {
	return &InstanceRepo{s: s, gameID: domain.GameValheim}
}

func (r *InstanceRepo) Upsert(inst domain.Instance) error {
	if r == nil || r.s == nil {
		return nil
	}
	if r.gameID == domain.GameValheim || inst.GameID == domain.GameValheim {
		return r.s.UpsertValheimInstance(toRecord(inst))
	}
	return r.s.UpsertInstance(toRecord(inst))
}

func (r *InstanceRepo) Get(number int) (*domain.Instance, error) {
	if r == nil || r.s == nil {
		return nil, nil
	}
	if r.gameID == domain.GameValheim {
		rec, err := r.s.GetValheimInstance(number)
		if err != nil || rec == nil {
			return nil, err
		}
		inst := fromRecord(*rec, domain.GameValheim)
		return &inst, nil
	}
	rec, err := r.s.GetInstance(number)
	if err != nil || rec == nil {
		return nil, err
	}
	inst := fromRecord(*rec, domain.GameMinecraft)
	return &inst, nil
}

func (r *InstanceRepo) List() ([]domain.Instance, error) {
	if r == nil || r.s == nil {
		return nil, nil
	}
	if r.gameID == domain.GameValheim {
		recs, err := r.s.ListValheimInstances()
		if err != nil {
			return nil, err
		}
		out := make([]domain.Instance, 0, len(recs))
		for _, rec := range recs {
			out = append(out, fromRecord(rec, domain.GameValheim))
		}
		return out, nil
	}
	recs, err := r.s.ListInstances()
	if err != nil {
		return nil, err
	}
	out := make([]domain.Instance, 0, len(recs))
	for _, rec := range recs {
		out = append(out, fromRecord(rec, domain.GameMinecraft))
	}
	return out, nil
}

func (r *InstanceRepo) UpdateState(number int, state domain.InstanceState) error {
	if r == nil || r.s == nil {
		return nil
	}
	if r.gameID == domain.GameValheim {
		return r.s.UpdateValheimInstanceState(number, string(state))
	}
	return r.s.UpdateInstanceState(number, string(state))
}

func (r *InstanceRepo) Delete(number int) error {
	if r == nil || r.s == nil {
		return nil
	}
	if r.gameID == domain.GameValheim {
		return r.s.DeleteValheimInstance(number)
	}
	return r.s.DeleteInstance(number)
}

func fromRecord(rec InstanceRecord, gameID domain.GameID) domain.Instance {
	inst := domain.Instance{
		GameID:     gameID,
		Number:     rec.Number,
		Name:       rec.Name,
		Slug:       rec.Slug,
		Seed:       rec.Seed,
		Password:   rec.Password,
		Loader:     domain.NormalizeLoader(rec.Loader),
		Source:     domain.NormalizeSource(rec.Source),
		MCVersion:  rec.MCVersion,
		Tier:       domain.NormalizeTier(rec.Tier),
		State:      domain.InstanceState(rec.State),
		MOTD:       rec.MOTD,
		Difficulty: rec.Difficulty,
		Gamemode:   rec.Gamemode,
		WorldType:  rec.WorldType,
		MaxPlayers: rec.MaxPlayers,
		LBIP:       rec.LBIP,
		CreatedAt:  rec.CreatedAt,
		LastUsed:   rec.LastUsed,
	}
	if gameID == domain.GameMinecraft && inst.Source == domain.SourceModpack && rec.PackRef != "" {
		provider := domain.Provider(rec.PackProvider)
		if provider == "" {
			provider = domain.ProviderCurseForge
		}
		inst.Pack = &domain.Pack{Provider: provider, Ref: rec.PackRef, Name: rec.Pack}
	}
	inst.EnsureDefaults("")
	return inst
}

func toRecord(inst domain.Instance) InstanceRecord {
	rec := InstanceRecord{
		Number:     inst.Number,
		Name:       inst.Name,
		Slug:       inst.Slug,
		Seed:       inst.Seed,
		Password:   inst.Password,
		Loader:     string(inst.Loader),
		Source:     string(inst.Source),
		MCVersion:  inst.MCVersion,
		Tier:       string(inst.Tier),
		State:      string(inst.State),
		MOTD:       inst.MOTD,
		Difficulty: inst.Difficulty,
		Gamemode:   inst.Gamemode,
		WorldType:  inst.WorldType,
		MaxPlayers: inst.MaxPlayers,
		LBIP:       inst.LBIP,
		CreatedAt:  inst.CreatedAt,
		LastUsed:   inst.LastUsed,
	}
	if inst.Pack != nil {
		rec.Pack = inst.Pack.Name
		rec.PackProvider = string(inst.Pack.Provider)
		rec.PackRef = inst.Pack.Ref
	}
	return rec
}

// Compile-time proof this adapter satisfies the port. Kept here rather than in
// ports/ so the port package stays free of adapter imports.
var _ interface {
	Upsert(domain.Instance) error
	Get(int) (*domain.Instance, error)
	List() ([]domain.Instance, error)
	UpdateState(int, domain.InstanceState) error
	Delete(int) error
} = (*InstanceRepo)(nil)
