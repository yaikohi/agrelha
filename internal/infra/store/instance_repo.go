package store

import (
	"fmt"

	"agrelha/internal/domain"
)

// InstanceRepo implements ports.InstanceRepository. It owns the record shape
// and the conversion to and from domain.Instance, so callers never see
// InstanceRecord — that type is a storage detail, not a domain concept.
type InstanceRepo struct {
	s       *Store
	profile domain.GameProfile
}

func NewInstanceRepo(s *Store) *InstanceRepo {
	return NewGameInstanceRepo(s, domain.MinecraftProfile)
}

func NewValheimInstanceRepo(s *Store) *InstanceRepo {
	return NewGameInstanceRepo(s, domain.ValheimProfile)
}

func NewGameInstanceRepo(s *Store, profile domain.GameProfile) *InstanceRepo {
	return &InstanceRepo{s: s, profile: profile}
}

func (r *InstanceRepo) Upsert(inst domain.Instance) error {
	if r == nil || r.s == nil {
		return nil
	}
	if inst.GameID == "" {
		inst.GameID = r.profile.ID
	}
	return r.s.upsertInstanceRow(inst.GameID, toRecord(inst))
}

func (r *InstanceRepo) Get(number int) (*domain.Instance, error) {
	if r == nil || r.s == nil {
		return nil, nil
	}
	rec, err := r.s.getInstanceRow(r.profile.ID, number)
	if err != nil || rec == nil {
		return nil, err
	}
	inst := fromRecord(*rec, r.profile.ID)
	return &inst, nil
}

func (r *InstanceRepo) List() ([]domain.Instance, error) {
	if r == nil || r.s == nil {
		return nil, nil
	}
	recs, err := r.s.listInstanceRows(r.profile.ID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Instance, 0, len(recs))
	for _, rec := range recs {
		out = append(out, fromRecord(rec, r.profile.ID))
	}
	return out, nil
}

func (r *InstanceRepo) UpdateState(number int, state domain.InstanceState) error {
	if r == nil || r.s == nil {
		return nil
	}
	return r.s.updateInstanceRowState(r.profile.ID, number, string(state))
}

func (r *InstanceRepo) Delete(number int) error {
	if r == nil || r.s == nil {
		return nil
	}
	return r.s.deleteInstanceRow(r.profile.ID, number)
}

type GameRecordMapper struct {
	ToRecord   func(domain.Instance, *InstanceRecord)
	FromRecord func(InstanceRecord, *domain.Instance)
}

var gameRecordMappers = map[domain.GameID]GameRecordMapper{
	domain.GameMinecraft: {
		ToRecord: func(inst domain.Instance, rec *InstanceRecord) {
			if inst.Minecraft != nil {
				rec.Loader = string(inst.Minecraft.Loader)
				rec.MCVersion = inst.Minecraft.MCVersion
				rec.Difficulty = inst.Minecraft.Difficulty
				rec.Gamemode = inst.Minecraft.Gamemode
				rec.WorldType = inst.Minecraft.WorldType
				rec.Seed = inst.Minecraft.Seed
				rec.HeapInitGiB = inst.Minecraft.HeapInitGiB
				if inst.Minecraft.Pack != nil {
					rec.Pack = inst.Minecraft.Pack.Name
					rec.PackProvider = string(inst.Minecraft.Pack.Provider)
					rec.PackRef = inst.Minecraft.Pack.Ref
				}
			}
		},
		FromRecord: func(rec InstanceRecord, inst *domain.Instance) {
			inst.Minecraft = &domain.MinecraftConfig{
				Loader:      domain.NormalizeLoader(rec.Loader),
				MCVersion:   rec.MCVersion,
				Difficulty:  rec.Difficulty,
				Gamemode:    rec.Gamemode,
				WorldType:   rec.WorldType,
				Seed:        rec.Seed,
				HeapInitGiB: rec.HeapInitGiB,
			}
			if inst.Source == domain.SourceModpack && rec.PackRef != "" {
				provider := domain.Provider(rec.PackProvider)
				if provider == "" {
					provider = domain.ProviderCurseForge
				}
				inst.Minecraft.Pack = &domain.Pack{Provider: provider, Ref: rec.PackRef, Name: rec.Pack}
			}
		},
	},
	domain.GameGMod: {
		ToRecord: func(inst domain.Instance, rec *InstanceRecord) {
			if inst.GMod == nil {
				return
			}
			rec.Gamemode = inst.GMod.Gamemode
			rec.Map = inst.GMod.Map
			rec.Password = inst.GMod.Password
			if inst.GMod.Pack != nil {
				rec.Pack = inst.GMod.Pack.Name
				rec.PackProvider = string(inst.GMod.Pack.Provider)
				rec.PackRef = inst.GMod.Pack.Ref
			}
		},
		FromRecord: func(rec InstanceRecord, inst *domain.Instance) {
			inst.GMod = &domain.GModConfig{
				Gamemode: rec.Gamemode,
				Map:      rec.Map,
				Password: rec.Password,
			}
			if rec.PackRef != "" {
				provider := domain.Provider(rec.PackProvider)
				if provider == "" {
					provider = domain.ProviderSteamWorkshop
				}
				inst.GMod.Pack = &domain.Pack{Provider: provider, Ref: rec.PackRef, Name: rec.Pack}
			}
		},
	},
	domain.GameValheim: {
		ToRecord: func(inst domain.Instance, rec *InstanceRecord) {
			if inst.Valheim != nil {
				rec.Password = inst.Valheim.Password
				rec.Seed = inst.Valheim.Seed
			}
		},
		FromRecord: func(rec InstanceRecord, inst *domain.Instance) {
			inst.Valheim = &domain.ValheimConfig{
				Password: rec.Password,
				Seed:     rec.Seed,
			}
		},
	},
}

func mapCoreFromRecord(rec InstanceRecord, gameID domain.GameID) domain.Instance {
	return domain.Instance{
		GameID: gameID,
		Number: rec.Number,
		Name:   rec.Name,
		Slug:   rec.Slug,
		Source: domain.NormalizeSource(rec.Source),
		Tier:   domain.NormalizeTier(rec.Tier),
		Resources: domain.Resources{
			MemRequestGiB:   rec.MemRequestGiB,
			MemLimitGiB:     rec.MemLimitGiB,
			CPURequestMilli: rec.CPURequestMilli,
			CPULimitMilli:   rec.CPULimitMilli,
		},
		State:      domain.InstanceState(rec.State),
		MOTD:       rec.MOTD,
		MaxPlayers: rec.MaxPlayers,
		LBIP:       rec.LBIP,
		CreatedBy:  rec.CreatedBy,
		CreatedAt:  rec.CreatedAt,
		LastUsed:   rec.LastUsed,
	}
}

func mapCoreToRecord(inst domain.Instance) InstanceRecord {
	return InstanceRecord{
		Number:          inst.Number,
		Name:            inst.Name,
		Slug:            inst.Slug,
		Source:          string(inst.Source),
		Tier:            string(inst.Tier),
		MemRequestGiB:   inst.Resources.MemRequestGiB,
		MemLimitGiB:     inst.Resources.MemLimitGiB,
		CPURequestMilli: inst.Resources.CPURequestMilli,
		CPULimitMilli:   inst.Resources.CPULimitMilli,
		State:           string(inst.State),
		MOTD:            inst.MOTD,
		MaxPlayers:      inst.MaxPlayers,
		LBIP:            inst.LBIP,
		CreatedBy:       inst.CreatedBy,
		CreatedAt:       inst.CreatedAt,
		LastUsed:        inst.LastUsed,
	}
}

func fromRecord(rec InstanceRecord, gameID domain.GameID) domain.Instance {
	inst := mapCoreFromRecord(rec, gameID)
	if mapper, ok := gameRecordMappers[gameID]; ok && mapper.FromRecord != nil {
		mapper.FromRecord(rec, &inst)
	}
	profile, ok := domain.ProfileFor(gameID)
	if !ok {
		panic(fmt.Sprintf("unknown or unregistered game ID %q", gameID))
	}
	inst.EnsureDefaults(profile, "")
	return inst
}

func toRecord(inst domain.Instance) InstanceRecord {
	rec := mapCoreToRecord(inst)
	if mapper, ok := gameRecordMappers[inst.GameID]; ok && mapper.ToRecord != nil {
		mapper.ToRecord(inst, &rec)
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
