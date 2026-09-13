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
	Password     string
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

func (s *Store) UpsertValheimInstance(inst InstanceRecord) error {
	_, err := s.db.Exec(`
		INSERT INTO valheim_instances (
			number, name, slug, seed, password, tier, state, motd, max_players, lb_ip, source, last_used
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(number) DO UPDATE SET
			name        = excluded.name,
			slug        = excluded.slug,
			seed        = excluded.seed,
			password    = excluded.password,
			tier        = excluded.tier,
			state       = excluded.state,
			motd        = excluded.motd,
			max_players = excluded.max_players,
			lb_ip       = excluded.lb_ip,
			source      = excluded.source,
			last_used   = CURRENT_TIMESTAMP`,
		inst.Number, inst.Name, inst.Slug, inst.Seed, inst.Password,
		inst.Tier, inst.State, inst.MOTD, inst.MaxPlayers, inst.LBIP, inst.Source,
	)
	return err
}

func (s *Store) GetValheimInstance(number int) (*InstanceRecord, error) {
	row := s.db.QueryRow(`
		SELECT number, name, slug, COALESCE(seed,''), COALESCE(password,''),
		       tier, state, COALESCE(motd,''), COALESCE(max_players,10),
		       COALESCE(lb_ip,''), COALESCE(source,''), created_at, last_used
		FROM valheim_instances WHERE number = ?`, number)

	var inst InstanceRecord
	var created, used any
	err := row.Scan(
		&inst.Number, &inst.Name, &inst.Slug, &inst.Seed, &inst.Password,
		&inst.Tier, &inst.State, &inst.MOTD, &inst.MaxPlayers,
		&inst.LBIP, &inst.Source, &created, &used,
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

func (s *Store) ListValheimInstances() ([]InstanceRecord, error) {
	rows, err := s.db.Query(`
		SELECT number, name, slug, COALESCE(seed,''), COALESCE(password,''),
		       tier, state, COALESCE(motd,''), COALESCE(max_players,10),
		       COALESCE(lb_ip,''), COALESCE(source,''), created_at, last_used
		FROM valheim_instances ORDER BY number ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []InstanceRecord
	for rows.Next() {
		var inst InstanceRecord
		var created, used any
		if err := rows.Scan(
			&inst.Number, &inst.Name, &inst.Slug, &inst.Seed, &inst.Password,
			&inst.Tier, &inst.State, &inst.MOTD, &inst.MaxPlayers,
			&inst.LBIP, &inst.Source, &created, &used,
		); err != nil {
			return nil, err
		}
		inst.CreatedAt = asTime(created)
		inst.LastUsed = asTime(used)
		out = append(out, inst)
	}
	return out, rows.Err()
}

func (s *Store) UpdateValheimInstanceState(number int, state string) error {
	_, err := s.db.Exec(`UPDATE valheim_instances SET state = ?, last_used = CURRENT_TIMESTAMP WHERE number = ?`, state, number)
	return err
}

func (s *Store) DeleteValheimInstance(number int) error {
	_, err := s.db.Exec(`DELETE FROM valheim_instances WHERE number = ?`, number)
	return err
}

// ValheimInstancesMissingSource lists instance numbers whose Source column was
// never written. It exists because the repository normalises an empty Source to
// modlist on read, so a caller can never tell "unset" from "explicitly modded" -
// which silently defeated the one-time backfill.
func (s *Store) ValheimInstancesMissingSource() ([]int, error) {
	rows, err := s.db.Query(`SELECT number FROM valheim_instances WHERE COALESCE(source,'') = '' ORDER BY number`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
