// Package journald tails the systemd journal for application log events
// and converts matching entries into governance packets.
//
// Only log entries that match configured patterns (e.g. secret access,
// auth failures, deployment signals) are captured. Noisy operational logs
// are filtered at the journald cursor level, not in Sentinel.
//
// M6 implementation milestone.
package journald
