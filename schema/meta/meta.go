// Package meta holds the _meta extension map that every ACP schema version
// reserves on its requests, responses and notifications.
//
// See protocol docs: [Extensibility](https://agentclientprotocol.com/protocol/extensibility)
package meta

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
)

// Meta is a _meta object. Values stay as raw JSON so that numbers, nesting and
// member order survive a decode/encode cycle; Set and Get convert them.
type Meta map[string]jsontext.Value

// Of builds a Meta from Go values, encoding each one.
func Of(values map[string]any) (Meta, error) {
	m := make(Meta, len(values))
	for key, v := range values {
		if err := m.Set(key, v); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// Set encodes v and stores it under key, allocating the map if needed.
func (m *Meta) Set(key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("_meta %q: %w", key, err)
	}
	if *m == nil {
		*m = Meta{}
	}
	(*m)[key] = raw
	return nil
}

// Get decodes the value stored under key. It reports false when key is absent.
func (m Meta) Get[T any](key string) (T, bool, error) {
	var v T
	raw, ok := m[key]
	if !ok {
		return v, false, nil
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, true, fmt.Errorf("_meta %q: %w", key, err)
	}
	return v, true, nil
}
