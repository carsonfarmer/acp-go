package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Metadata represents the structure of meta.json
type Metadata struct {
	AgentMethods  map[string]string `json:"agentMethods"`
	ClientMethods map[string]string `json:"clientMethods"`
	Version       int               `json:"version"`
}

// LoadMetadata loads metadata from meta.json file
func LoadMetadata(filepath string) (*Metadata, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata file %s: %w", filepath, err)
	}

	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("failed to parse metadata file %s: %w", filepath, err)
	}

	return &meta, nil
}

// GetInternalTypes returns list of types that should be marked as internal
func (m *Metadata) GetInternalTypes() []string {
	return []string{
		"AgentRequest",
		"AgentResponse", 
		"AgentNotification",
		"ClientRequest",
		"ClientResponse",
		"ClientNotification",
	}
}