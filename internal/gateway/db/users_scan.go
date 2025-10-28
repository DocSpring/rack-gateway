package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// userScanner defines the interface for scanning SQL rows into a User struct.
// This allows the same helpers to work with sql.Row and sql.Rows.
type userScanner interface {
	Scan(dest ...interface{}) error
}

// scanUserBasicFields scans the basic user fields that are present in all user queries.
// This includes ID, email, name, roles, timestamps, suspended state, and MFA fields.
// Returns the user struct and any nullable fields that need additional processing.
func scanUserBasicFields(scanner userScanner) (*User, *userNullableFields, error) {
	var user User
	var fields userNullableFields

	err := scanner.Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&fields.rolesJSON,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.Suspended,
		&user.MFAEnrolled,
		&user.MFAEnforcedAt,
		&user.PreferredMFAMethod,
		&user.LockedAt,
		&fields.lockedReason,
		&user.LockedByUserID,
	)
	if err != nil {
		return nil, nil, err
	}

	return &user, &fields, nil
}

// scanUserWithLockedBy scans a user row that includes locked_by user information.
// This is used by GetUser which joins with the users table to get locked_by details.
func scanUserWithLockedBy(scanner userScanner) (*User, error) {
	var user User
	var rolesJSON string
	var lockedReason, lockedByEmail, lockedByName sql.NullString

	err := scanner.Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&rolesJSON,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.Suspended,
		&user.MFAEnrolled,
		&user.MFAEnforcedAt,
		&user.PreferredMFAMethod,
		&user.LockedAt,
		&lockedReason,
		&user.LockedByUserID,
		&lockedByEmail,
		&lockedByName,
	)
	if err != nil {
		return nil, err
	}

	if err := assignRolesAndLockedReason(&user, rolesJSON, lockedReason); err != nil {
		return nil, err
	}
	if lockedByEmail.Valid {
		user.LockedByEmail = lockedByEmail.String
	}
	if lockedByName.Valid {
		user.LockedByName = lockedByName.String
	}

	return &user, nil
}

// scanUserWithCreator scans a user row that includes creator information.
// This is used by ListUsers which joins with user_resources to get creator details.
func scanUserWithCreator(scanner userScanner) (*User, error) {
	var user User
	var rolesJSON string
	var lockedReason sql.NullString
	var creatorID sql.NullInt64
	var creatorEmail, creatorName sql.NullString

	// Note: This scan pattern matches the ListUsers SELECT which excludes MFA fields
	err := scanner.Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&rolesJSON,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.Suspended,
		&user.LockedAt,
		&lockedReason,
		&user.LockedByUserID,
		&creatorID,
		&creatorEmail,
		&creatorName,
	)
	if err != nil {
		return nil, err
	}

	if err := assignRolesAndLockedReason(&user, rolesJSON, lockedReason); err != nil {
		return nil, err
	}

	if creatorID.Valid {
		id := creatorID.Int64
		user.CreatedByUserID = &id
	}
	if creatorEmail.Valid {
		user.CreatedByEmail = creatorEmail.String
	}
	if creatorName.Valid {
		user.CreatedByName = creatorName.String
	}

	return &user, nil
}

// userNullableFields holds nullable SQL fields that need special handling.
type userNullableFields struct {
	rolesJSON    string
	lockedReason sql.NullString
}

// finalizeUserFields completes user struct initialization by parsing JSON roles
// and handling nullable fields.
func finalizeUserFields(user *User, fields *userNullableFields) error {
	if err := json.Unmarshal([]byte(fields.rolesJSON), &user.Roles); err != nil {
		return fmt.Errorf("failed to unmarshal roles: %w", err)
	}

	if fields.lockedReason.Valid {
		user.LockedReason = fields.lockedReason.String
	}

	return nil
}

func assignRolesAndLockedReason(user *User, rolesJSON string, lockedReason sql.NullString) error {
	if err := json.Unmarshal([]byte(rolesJSON), &user.Roles); err != nil {
		return fmt.Errorf("failed to unmarshal roles: %w", err)
	}

	if lockedReason.Valid {
		user.LockedReason = lockedReason.String
	}

	return nil
}
