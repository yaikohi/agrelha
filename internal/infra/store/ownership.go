package store

import (
	"context"
	"fmt"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

var _ ports.InstanceOwners = (*Store)(nil)

func (s *Store) InstancesCreatedBy(ctx context.Context, subject string) ([]domain.InstanceID, error) {
	if s == nil || s.db == nil || subject == "" {
		return nil, nil
	}
	var out []domain.InstanceID
	for _, profile := range domain.Profiles() {
		rows, err := s.db.QueryContext(ctx,
			`SELECT number FROM `+profile.Table+` WHERE created_by = ?`, subject)
		if err != nil {
			return nil, fmt.Errorf("list instances created by %q in %s: %w", subject, profile.Table, err)
		}
		for rows.Next() {
			var number int
			if err := rows.Scan(&number); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan instance created by %q: %w", subject, err)
			}
			out = append(out, domain.NewInstanceID(profile.ID, number))
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("list instances created by %q in %s: %w", subject, profile.Table, err)
		}
		rows.Close()
	}
	return out, nil
}
