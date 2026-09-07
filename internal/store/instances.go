package store

import (
	"database/sql"
	"time"
)

type InstanceRecord struct {
	Number       int
	Name         string
	Slug         string
	Seed         string
	Loader       string
	Source       string
	Pack         string
	PackProvider string
	PackRef      string
	MCVersion    string
	Tier         string
	State        string
	MOTD         string
	Difficulty   string
	Gamemode     string
	WorldType    string
	MaxPlayers   int
	LBIP         string
	CreatedAt    time.Time
	LastUsed     time.Time
}

func (s *Store) UpsertInstance(inst InstanceRecord) error {
	_, err := s.db.Exec(`
		INSERT INTO mc_instances (
			number, name, slug, seed, loader, source, pack, pack_provider, pack_ref,
			mc_version, tier, state, motd, difficulty, gamemode, world_type, max_players, lb_ip, last_used
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(number) DO UPDATE SET
			name          = excluded.name,
			slug          = excluded.slug,
			seed          = excluded.seed,
			loader        = excluded.loader,
			source        = excluded.source,
			pack          = excluded.pack,
			pack_provider = excluded.pack_provider,
			pack_ref      = excluded.pack_ref,
			mc_version    = excluded.mc_version,
			tier          = excluded.tier,
			state         = excluded.state,
			motd          = excluded.motd,
			difficulty    = excluded.difficulty,
			gamemode      = excluded.gamemode,
			world_type    = excluded.world_type,
			max_players   = excluded.max_players,
			lb_ip         = excluded.lb_ip,
			last_used     = CURRENT_TIMESTAMP`,
		inst.Number, inst.Name, inst.Slug, inst.Seed, inst.Loader, inst.Source,
		inst.Pack, inst.PackProvider, inst.PackRef, inst.MCVersion, inst.Tier,
		inst.State, inst.MOTD, inst.Difficulty, inst.Gamemode, inst.WorldType,
		inst.MaxPlayers, inst.LBIP,
	)
	return err
}

func (s *Store) GetInstance(number int) (*InstanceRecord, error) {
	row := s.db.QueryRow(`
		SELECT number, name, slug, COALESCE(seed,''), loader, source,
		       COALESCE(pack,''), COALESCE(pack_provider,''), COALESCE(pack_ref,''),
		       COALESCE(mc_version,''), tier, state, COALESCE(motd,''),
		       COALESCE(difficulty,'normal'), COALESCE(gamemode,'survival'),
		       COALESCE(world_type,'default'), COALESCE(max_players,20),
		       COALESCE(lb_ip,''), created_at, last_used
		FROM mc_instances WHERE number = ?`, number)

	var inst InstanceRecord
	var created, used any
	err := row.Scan(
		&inst.Number, &inst.Name, &inst.Slug, &inst.Seed, &inst.Loader, &inst.Source,
		&inst.Pack, &inst.PackProvider, &inst.PackRef, &inst.MCVersion, &inst.Tier,
		&inst.State, &inst.MOTD, &inst.Difficulty, &inst.Gamemode, &inst.WorldType,
		&inst.MaxPlayers, &inst.LBIP, &created, &used,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	inst.CreatedAt = asTime(created)
	inst.LastUsed = asTime(used)
	return &inst, nil
}

func (s *Store) ListInstances() ([]InstanceRecord, error) {
	rows, err := s.db.Query(`
		SELECT number, name, slug, COALESCE(seed,''), loader, source,
		       COALESCE(pack,''), COALESCE(pack_provider,''), COALESCE(pack_ref,''),
		       COALESCE(mc_version,''), tier, state, COALESCE(motd,''),
		       COALESCE(difficulty,'normal'), COALESCE(gamemode,'survival'),
		       COALESCE(world_type,'default'), COALESCE(max_players,20),
		       COALESCE(lb_ip,''), created_at, last_used
		FROM mc_instances ORDER BY number ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []InstanceRecord
	for rows.Next() {
		var inst InstanceRecord
		var created, used any
		if err := rows.Scan(
			&inst.Number, &inst.Name, &inst.Slug, &inst.Seed, &inst.Loader, &inst.Source,
			&inst.Pack, &inst.PackProvider, &inst.PackRef, &inst.MCVersion, &inst.Tier,
			&inst.State, &inst.MOTD, &inst.Difficulty, &inst.Gamemode, &inst.WorldType,
			&inst.MaxPlayers, &inst.LBIP, &created, &used,
		); err != nil {
			return nil, err
		}
		inst.CreatedAt = asTime(created)
		inst.LastUsed = asTime(used)
		out = append(out, inst)
	}
	return out, rows.Err()
}

func (s *Store) UpdateInstanceState(number int, state string) error {
	_, err := s.db.Exec(`UPDATE mc_instances SET state = ?, last_used = CURRENT_TIMESTAMP WHERE number = ?`, state, number)
	return err
}

func (s *Store) DeleteInstance(number int) error {
	_, err := s.db.Exec(`DELETE FROM mc_instances WHERE number = ?`, number)
	return err
}
