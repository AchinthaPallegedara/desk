// Package migrations holds the SQL schema migrations (goose format).
//
// The schema is defined here in SQL, not with GORM AutoMigrate: composite
// tenant FKs, RLS policies, partial unique indexes, deferrable constraints and
// immutability triggers (see docs/data-model.md) can't be expressed in GORM tags.
// GORM models must mirror these tables, not define them.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
