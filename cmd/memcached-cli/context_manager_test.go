package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTemporaryContextIsNotPersisted(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	manager := &contextManager{
		contexts: map[string]*Context{
			"saved": {Name: "saved", Servers: "saved.example:11211"},
		},
		current:         "saved",
		configDir:       configDir,
		path:            "config.json",
		historyMaxLines: 100,
		historyEnabled:  true,
	}

	require.NoError(t, manager.addTemporaryContext("temporary.example:11211", "rendezvous"))
	assert.Equal(t, "temporary", manager.current)
	require.NoError(t, manager.save())

	data, err := os.ReadFile(filepath.Join(configDir, "config.json"))
	require.NoError(t, err)
	var stored struct {
		Current  string              `json:"current"`
		Contexts map[string]*Context `json:"contexts"`
	}
	require.NoError(t, json.Unmarshal(data, &stored))
	assert.Equal(t, "saved", stored.Current)
	assert.Contains(t, stored.Contexts, "saved")
	assert.NotContains(t, stored.Contexts, "temporary")
}
