package models

import (
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
)

// The domain speaks strings; Postgres speaks uuid. Converting here rather than
// in app/domain is what lets the domain package import nothing third-party.

func parseUUID(s string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		return pgtype.UUID{}, fmt.Errorf("models: %q is not a uuid: %w", s, err)
	}
	return id, nil
}

func uuidString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	v, err := id.Value()
	if err != nil {
		return ""
	}
	s, _ := v.(string)
	return s
}
