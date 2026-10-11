package db

import "github.com/google/uuid"

// isPublicID reports whether s can be a public ID (a UUID in its standard 36-character form). Querying a UUID
// column with anything else is a Postgres error, which would surface as a 500 instead of "not found". uuid.Parse
// alone also accepts forms Postgres refuses, such as "urn:uuid:..." or any wrapper around the 36 characters.
func isPublicID(s string) bool {
	if len(s) != 36 {
		return false
	}
	_, err := uuid.Parse(s)
	return err == nil
}
