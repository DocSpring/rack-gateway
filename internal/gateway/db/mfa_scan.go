package db

import (
	"database/sql"
	"encoding/json"
	"log"
)

// scanMFAMethod scans a single MFA method row into an MFAMethod struct.
// It handles nullable fields and JSON unmarshalling consistently.
func scanMFAMethod(scanner interface {
	Scan(dest ...interface{}) error
}) (*MFAMethod, error) {
	var method MFAMethod
	var label sql.NullString
	var secret sql.NullString
	var transports sql.NullString
	var metadata sql.NullString
	var confirmed sql.NullTime
	var lastUsed sql.NullTime

	err := scanner.Scan(
		&method.ID, &method.UserID, &method.Type, &label, &secret,
		&method.CredentialID, &method.PublicKey, &transports, &metadata,
		&method.CreatedAt, &confirmed, &lastUsed,
	)
	if err != nil {
		return nil, err
	}

	if label.Valid {
		method.Label = label.String
	}
	if secret.Valid {
		method.Secret = secret.String
	}
	if transports.Valid {
		var arr []string
		if err := json.Unmarshal([]byte(transports.String), &arr); err != nil {
			log.Printf("WARN: failed to unmarshal MFA transports for method %d: %v", method.ID, err)
		} else {
			method.Transports = arr
		}
	}
	if metadata.Valid {
		method.Metadata = []byte(metadata.String)
	}
	if confirmed.Valid {
		t := confirmed.Time
		method.ConfirmedAt = &t
	}
	if lastUsed.Valid {
		t := lastUsed.Time
		method.LastUsedAt = &t
	}

	return &method, nil
}

// scanTrustedDevice scans a single trusted device row into a TrustedDevice struct.
// It handles nullable fields and JSON unmarshalling consistently.
func scanTrustedDevice(scanner interface {
	Scan(dest ...interface{}) error
}) (*TrustedDevice, error) {
	var device TrustedDevice
	var ipFirst sql.NullString
	var ipLast sql.NullString
	var ua sql.NullString
	var revoked sql.NullTime
	var reason sql.NullString
	var metadata sql.NullString

	err := scanner.Scan(
		&device.ID, &device.UserID, &device.DeviceID, &device.TokenHash,
		&device.CreatedAt, &device.UpdatedAt, &device.ExpiresAt, &device.LastUsedAt,
		&ipFirst, &ipLast, &ua, &revoked, &reason, &metadata,
	)
	if err != nil {
		return nil, err
	}

	if ipFirst.Valid {
		device.IPFirst = ipFirst.String
	}
	if ipLast.Valid {
		device.IPLast = ipLast.String
	}
	if ua.Valid {
		device.UserAgentHash = ua.String
	}
	if revoked.Valid {
		t := revoked.Time
		device.RevokedAt = &t
	}
	if reason.Valid {
		device.RevokedReason = reason.String
	}
	if metadata.Valid {
		device.Metadata = json.RawMessage(metadata.String)
	}

	return &device, nil
}
