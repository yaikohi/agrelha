package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"agrelha/internal/domain"
)

// A column binds one database column to one InstanceRecord field. Tables are
// described as column lists so adding a game means declaring its table, not
// adding a branch to five methods.
type instanceColumn struct {
	name string
	sel  string
	bind func(*InstanceRecord) any
	dest func(*InstanceRecord) any
}

func textCol(name string, bind, dest func(*InstanceRecord) any) instanceColumn {
	return instanceColumn{name: name, sel: fmt.Sprintf("COALESCE(%s,'')", name), bind: bind, dest: dest}
}

func intCol(name string, bind, dest func(*InstanceRecord) any) instanceColumn {
	return instanceColumn{name: name, sel: fmt.Sprintf("COALESCE(%s,0)", name), bind: bind, dest: dest}
}

type instanceTable struct {
	name    string
	columns []instanceColumn
}

func coreInstanceColumns() []instanceColumn {
	return []instanceColumn{
		{name: "number", sel: "number",
			bind: func(r *InstanceRecord) any { return r.Number },
			dest: func(r *InstanceRecord) any { return &r.Number }},
		{name: "name", sel: "name",
			bind: func(r *InstanceRecord) any { return r.Name },
			dest: func(r *InstanceRecord) any { return &r.Name }},
		{name: "slug", sel: "slug",
			bind: func(r *InstanceRecord) any { return r.Slug },
			dest: func(r *InstanceRecord) any { return &r.Slug }},
		textCol("seed",
			func(r *InstanceRecord) any { return r.Seed },
			func(r *InstanceRecord) any { return &r.Seed }),
		textCol("source",
			func(r *InstanceRecord) any { return r.Source },
			func(r *InstanceRecord) any { return &r.Source }),
		{name: "tier", sel: "tier",
			bind: func(r *InstanceRecord) any { return r.Tier },
			dest: func(r *InstanceRecord) any { return &r.Tier }},
		{name: "state", sel: "state",
			bind: func(r *InstanceRecord) any { return r.State },
			dest: func(r *InstanceRecord) any { return &r.State }},
		textCol("motd",
			func(r *InstanceRecord) any { return r.MOTD },
			func(r *InstanceRecord) any { return &r.MOTD }),
		intCol("max_players",
			func(r *InstanceRecord) any { return r.MaxPlayers },
			func(r *InstanceRecord) any { return &r.MaxPlayers }),
		textCol("lb_ip",
			func(r *InstanceRecord) any { return r.LBIP },
			func(r *InstanceRecord) any { return &r.LBIP }),
		textCol("created_by",
			func(r *InstanceRecord) any { return r.CreatedBy },
			func(r *InstanceRecord) any { return &r.CreatedBy }),
		intCol("mem_request_gib",
			func(r *InstanceRecord) any { return r.MemRequestGiB },
			func(r *InstanceRecord) any { return &r.MemRequestGiB }),
		intCol("mem_limit_gib",
			func(r *InstanceRecord) any { return r.MemLimitGiB },
			func(r *InstanceRecord) any { return &r.MemLimitGiB }),
		intCol("cpu_request_milli",
			func(r *InstanceRecord) any { return r.CPURequestMilli },
			func(r *InstanceRecord) any { return &r.CPURequestMilli }),
		intCol("cpu_limit_milli",
			func(r *InstanceRecord) any { return r.CPULimitMilli },
			func(r *InstanceRecord) any { return &r.CPULimitMilli }),
	}
}

func minecraftColumns() []instanceColumn {
	return []instanceColumn{
		textCol("loader",
			func(r *InstanceRecord) any { return r.Loader },
			func(r *InstanceRecord) any { return &r.Loader }),
		textCol("pack",
			func(r *InstanceRecord) any { return r.Pack },
			func(r *InstanceRecord) any { return &r.Pack }),
		textCol("pack_provider",
			func(r *InstanceRecord) any { return r.PackProvider },
			func(r *InstanceRecord) any { return &r.PackProvider }),
		textCol("pack_ref",
			func(r *InstanceRecord) any { return r.PackRef },
			func(r *InstanceRecord) any { return &r.PackRef }),
		textCol("mc_version",
			func(r *InstanceRecord) any { return r.MCVersion },
			func(r *InstanceRecord) any { return &r.MCVersion }),
		{name: "difficulty", sel: "COALESCE(difficulty,'normal')",
			bind: func(r *InstanceRecord) any { return r.Difficulty },
			dest: func(r *InstanceRecord) any { return &r.Difficulty }},
		{name: "gamemode", sel: "COALESCE(gamemode,'survival')",
			bind: func(r *InstanceRecord) any { return r.Gamemode },
			dest: func(r *InstanceRecord) any { return &r.Gamemode }},
		{name: "world_type", sel: "COALESCE(world_type,'default')",
			bind: func(r *InstanceRecord) any { return r.WorldType },
			dest: func(r *InstanceRecord) any { return &r.WorldType }},
		intCol("heap_init_gib",
			func(r *InstanceRecord) any { return r.HeapInitGiB },
			func(r *InstanceRecord) any { return &r.HeapInitGiB }),
	}
}

func valheimColumns() []instanceColumn {
	return []instanceColumn{
		textCol("password",
			func(r *InstanceRecord) any { return r.Password },
			func(r *InstanceRecord) any { return &r.Password }),
	}
}

func gmodColumns() []instanceColumn {
	return []instanceColumn{
		textCol("pack",
			func(r *InstanceRecord) any { return r.Pack },
			func(r *InstanceRecord) any { return &r.Pack }),
		textCol("pack_provider",
			func(r *InstanceRecord) any { return r.PackProvider },
			func(r *InstanceRecord) any { return &r.PackProvider }),
		textCol("pack_ref",
			func(r *InstanceRecord) any { return r.PackRef },
			func(r *InstanceRecord) any { return &r.PackRef }),
		{name: "gamemode", sel: "COALESCE(gamemode,'sandbox')",
			bind: func(r *InstanceRecord) any { return r.Gamemode },
			dest: func(r *InstanceRecord) any { return &r.Gamemode }},
		{name: "map", sel: "COALESCE(map,'gm_construct')",
			bind: func(r *InstanceRecord) any { return r.Map },
			dest: func(r *InstanceRecord) any { return &r.Map }},
		textCol("password",
			func(r *InstanceRecord) any { return r.Password },
			func(r *InstanceRecord) any { return &r.Password }),
	}
}

var instanceTables = map[domain.GameID]instanceTable{
	domain.GameMinecraft: {name: "mc_instances", columns: append(coreInstanceColumns(), minecraftColumns()...)},
	domain.GameValheim:   {name: "valheim_instances", columns: append(coreInstanceColumns(), valheimColumns()...)},
	domain.GameGMod:      {name: "gmod_instances", columns: append(coreInstanceColumns(), gmodColumns()...)},
}

func tableFor(gameID domain.GameID) (instanceTable, error) {
	t, ok := instanceTables[gameID]
	if !ok {
		return instanceTable{}, fmt.Errorf("no instance table registered for game %q", gameID)
	}
	return t, nil
}

func (t instanceTable) selectList() string {
	parts := make([]string, 0, len(t.columns)+2)
	for _, c := range t.columns {
		parts = append(parts, c.sel)
	}
	return strings.Join(parts, ", ") + ", created_at, last_used"
}

func (t instanceTable) scanDests(rec *InstanceRecord, created, used *any) []any {
	out := make([]any, 0, len(t.columns)+2)
	for _, c := range t.columns {
		out = append(out, c.dest(rec))
	}
	return append(out, created, used)
}

func (t instanceTable) upsertSQL() string {
	names := make([]string, 0, len(t.columns))
	holes := make([]string, 0, len(t.columns))
	sets := make([]string, 0, len(t.columns))
	for _, c := range t.columns {
		names = append(names, c.name)
		holes = append(holes, "?")
		switch c.name {
		case "number":
		case "created_by":
			// First writer wins: a later save must not blank the creator.
			sets = append(sets, fmt.Sprintf(
				"created_by = CASE WHEN %s.created_by = '' THEN excluded.created_by ELSE %s.created_by END",
				t.name, t.name))
		default:
			sets = append(sets, fmt.Sprintf("%s = excluded.%s", c.name, c.name))
		}
	}
	sets = append(sets, "last_used = CURRENT_TIMESTAMP")

	return fmt.Sprintf(
		"INSERT INTO %s (%s, last_used) VALUES (%s, CURRENT_TIMESTAMP)\nON CONFLICT(number) DO UPDATE SET\n\t%s",
		t.name, strings.Join(names, ", "), strings.Join(holes, ", "), strings.Join(sets, ",\n\t"))
}

func (t instanceTable) binds(rec InstanceRecord) []any {
	out := make([]any, 0, len(t.columns))
	for _, c := range t.columns {
		out = append(out, c.bind(&rec))
	}
	return out
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

func (s *Store) upsertInstanceRow(gameID domain.GameID, rec InstanceRecord) error {
	t, err := tableFor(gameID)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(t.upsertSQL(), t.binds(rec)...); err != nil {
		return fmt.Errorf("upsert %s instance %d: %w", gameID, rec.Number, err)
	}
	return nil
}

func (s *Store) getInstanceRow(gameID domain.GameID, number int) (*InstanceRecord, error) {
	t, err := tableFor(gameID)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRow(
		fmt.Sprintf("SELECT %s FROM %s WHERE number = ?", t.selectList(), t.name), number)

	var rec InstanceRecord
	var created, used any
	if err := row.Scan(t.scanDests(&rec, &created, &used)...); err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get %s instance %d: %w", gameID, number, err)
	}
	rec.CreatedAt = asTime(created)
	rec.LastUsed = asTime(used)
	return &rec, nil
}

func (s *Store) listInstanceRows(gameID domain.GameID) ([]InstanceRecord, error) {
	t, err := tableFor(gameID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		fmt.Sprintf("SELECT %s FROM %s ORDER BY number ASC", t.selectList(), t.name))
	if err != nil {
		return nil, fmt.Errorf("list %s instances: %w", gameID, err)
	}
	defer rows.Close()

	var out []InstanceRecord
	for rows.Next() {
		var rec InstanceRecord
		var created, used any
		if err := rows.Scan(t.scanDests(&rec, &created, &used)...); err != nil {
			return nil, err
		}
		rec.CreatedAt = asTime(created)
		rec.LastUsed = asTime(used)
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Store) updateInstanceRowState(gameID domain.GameID, number int, state string) error {
	t, err := tableFor(gameID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		fmt.Sprintf("UPDATE %s SET state = ?, last_used = CURRENT_TIMESTAMP WHERE number = ?", t.name),
		state, number)
	return err
}

func (s *Store) deleteInstanceRow(gameID domain.GameID, number int) error {
	t, err := tableFor(gameID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(fmt.Sprintf("DELETE FROM %s WHERE number = ?", t.name), number)
	return err
}
