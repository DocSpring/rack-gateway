package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// buildRepositoryField is the build field the rack fills in for an external build: a registry URL for the client to
// push images to, which embeds the rack's own API password (provider/k8s/build.go). The gateway refuses external
// builds, and strips the field from every build it passes on in case a rack sets it anyway.
const buildRepositoryField = "repository"

// stripBuildRepository removes the repository field from a build response: a build object, or a list of them.
// Keys are matched without regard to case, as Go's JSON decoding (and so the Convox client) matches fields.
func stripBuildRepository(body []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return body, nil
	}
	switch trimmed[0] {
	case '{':
		return stripRepositoryField(trimmed)
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, err
		}
		for i, item := range items {
			if bytes.HasPrefix(bytes.TrimSpace(item), []byte("{")) {
				stripped, err := stripRepositoryField(item)
				if err != nil {
					return nil, err
				}
				items[i] = stripped
			}
		}
		return json.Marshal(items)
	default:
		if !json.Valid(trimmed) {
			return nil, errors.New("response is not valid JSON")
		}
		return body, nil // a scalar has no fields
	}
}

func stripRepositoryField(object []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(object, &fields); err != nil {
		return nil, err
	}
	for key := range fields {
		if strings.EqualFold(key, buildRepositoryField) {
			delete(fields, key)
		}
	}
	return json.Marshal(fields)
}
