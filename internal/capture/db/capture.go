// Package db captures database write events as governance packets.
//
// Integration paths:
//   - pgx/v5 tracer hook (Go apps using pgx)
//   - sqlhooks for database/sql
//   - Query log tail (agent-side capture for non-Go apps)
//
// All db packets use category="db" and mutating=true for INSERT/UPDATE/DELETE.
//
// M5 implementation milestone.
package db
