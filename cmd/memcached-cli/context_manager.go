package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/yeqown/memcached"
)

// contextManager handles multiple memcached contexts
type contextManager struct {
	mu              sync.RWMutex
	contexts        map[string]*Context
	current         string
	path            string
	historyMaxLines int
	historyEnabled  bool

	currentClient  memcached.Client
	historyManager *kvCommandHistoryManager
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
		mu:              sync.RWMutex{},
		contexts:        make(map[string]*Context, 4),
		current:         "",
		path:            path,
		historyMaxLines: 10000,
		historyEnabled:  true,
		currentClient:   nil,
	}

	if err := m.initialize(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return m, nil
}

func (m *contextManager) close() error {
	m.save()

	if m.currentClient != nil {
		return m.currentClient.Close()
	}

	return nil
}

// initialize load CLI config file and initialize current client while
// the current is not empty.
func (m *contextManager) initialize() error {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return err
	}

	var stored struct {
		Current         string              `json:"current"`
		HistoryMaxLines int                 `json:"history_max_lines"`
		HistoryEnabled  bool                `json:"history_enabled"`
		Contexts        map[string]*Context `json:"contexts"`
	}

	if err := json.Unmarshal(data, &stored); err != nil {
		return err
	}

	m.mu.Lock()
	m.contexts = stored.Contexts
	m.current = stored.Current
	m.historyMaxLines = stored.HistoryMaxLines
	m.historyEnabled = stored.HistoryEnabled
	if m.current != "" {
		m.currentClient, err = createClient(m.contexts[m.current])
		if err != nil {
			return err
		}
	}

	if m.historyEnabled {
		m.historyManager, err = newHistoryManager(m.historyEnabled, m.historyMaxLines)
		if err != nil {
			return err
		}
	}

	m.mu.Unlock()

	return nil
}

// save writes contexts to disk
func (m *contextManager) save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := json.MarshalIndent(struct {
		Current         string              `json:"current"`
		HistoryMaxLines int                 `json:"history_max_lines"`
		HistoryEnabled  bool                `json:"history_enabled"`
		Contexts        map[string]*Context `json:"contexts"`
	}{
		Current:         m.current,
		HistoryMaxLines: m.historyMaxLines,
		HistoryEnabled:  m.historyEnabled,
		Contexts:        m.contexts,
	}, "", "  ")

	logger.Debugf("saving context to %s, current=%s, contexts=%+v", m.path, m.current, m.contexts)

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

	return nil
	// return m.save()
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

func (m *contextManager) getClientWithContext(ctxName string) (memcached.Client, error) {
	if ctxName == "" {
		return m.getCurrentClient()
	}

	m.mu.RLock()
	ctx, exists := m.contexts[ctxName]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("context %s not found", ctxName)
	}
	client, err := createClient(ctx)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func (m *contextManager) getCurrentClient() (memcached.Client, error) {
	m.mu.RLock()
	if m.currentClient != nil {
		clientCopy := m.currentClient
		m.mu.RUnlock()
		return clientCopy, nil
	}
	m.mu.RUnlock()

	currentCtx, err := m.getCurrentContext()
	if err != nil {
		return nil, err
	}

	// re-init
	client, err := createClient(currentCtx)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.currentClient = client
	m.mu.Unlock()

	return client, nil
}

func (m *contextManager) getHistoryManager() *kvCommandHistoryManager {
	if !m.historyEnabled {
		return nil
	}

	return m.historyManager
}
