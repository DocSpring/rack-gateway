package db

import "github.com/google/uuid"

// isPublicID reports whether s can be a public ID (a UUID). Querying a UUID column with anything else is a
// Postgres error, which would surface as a 500 instead of "not found".
func isPublicID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}
