package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// contextManager handles multiple memcached contexts
type contextManager struct {
	contexts map[string]*Context
	current  string
	path     string
	mu       sync.RWMutex
}

// newContextManager creates a new context manager
func newContextManager() (*contextManager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(home, ".memcached-cli", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	m := &contextManager{
		contexts: make(map[string]*Context),
		path:     path,
	}

	if err := m.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return m, nil
}

// load reads contexts from disk
func (m *contextManager) load() error {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return err
	}

	var stored struct {
		Current  string              `json:"current"`
		Contexts map[string]*Context `json:"contexts"`
	}

	if err := json.Unmarshal(data, &stored); err != nil {
		return err
	}

	m.mu.Lock()
	m.contexts = stored.Contexts
	m.current = stored.Current
	m.mu.Unlock()

	return nil
}

// save writes contexts to disk
func (m *contextManager) save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := json.MarshalIndent(struct {
		Current  string              `json:"current"`
		Contexts map[string]*Context `json:"contexts"`
	}{
		Current:  m.current,
		Contexts: m.contexts,
	}, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.path, data, 0644)
}

// CreateContext creates a new context with the given name and configuration
func (m *contextManager) newContext(name, servers string, config *clientConfig) error {
	m.mu.Lock()

	if _, exists := m.contexts[name]; exists {
		m.mu.Unlock()
		return fmt.Errorf("context %s already exists", name)
	}

	if config == nil {
		defaultConfig := defaultConfig()
		config = &defaultConfig
	}

	ctx := &Context{
		Name:      name,
		Servers:   servers,
		Config:    *config,
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
	}

	m.contexts[name] = ctx
	if m.current == "" {
		m.current = name
	}
	m.mu.Unlock()

	return m.save()
}

// GetContext returns the context with the given name
func (m *contextManager) getContext(name string) (*Context, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ctx, exists := m.contexts[name]
	if !exists {
		return nil, fmt.Errorf("context %s not found", name)
	}
	return ctx, nil
}

// UseContext sets the current context
func (m *contextManager) useContext(name string) error {
	m.mu.Lock()

	if _, exists := m.contexts[name]; !exists {
		m.mu.Unlock()
		return fmt.Errorf("context %s not found", name)
	}

	m.current = name
	m.contexts[name].LastUsed = time.Now()
	m.mu.Unlock()

	return m.save()
}

// DeleteContext removes a context
func (m *contextManager) deleteContext(name string) error {
	m.mu.Lock()

	if _, exists := m.contexts[name]; !exists {
		m.mu.Unlock()
		return fmt.Errorf("context %s not found", name)
	}

	delete(m.contexts, name)
	if m.current == name {
		m.current = ""
	}
	m.mu.Unlock()

	return m.save()
}

// ListContexts returns all context names
func (m *contextManager) listContexts() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.contexts))
	for name := range m.contexts {
		names = append(names, name)
	}
	return names
}

// GetCurrentContext returns the current context
func (m *contextManager) getCurrentContext() (*Context, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.current == "" {
		return nil, fmt.Errorf("no context selected")
	}

	return m.contexts[m.current], nil
}
