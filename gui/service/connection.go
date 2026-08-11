package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	memcached "github.com/yeqown/memcached"
)

const configFileName = "contexts.json"

// ConnectionService manages memcached contexts and connections.
type ConnectionService struct {
	mu        sync.Mutex
	client    memcached.Client
	configDir string
	connected bool
	activeCtx *Context
}

// NewConnectionService creates a new ConnectionService.
func NewConnectionService() (*ConnectionService, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get config dir: %w", err)
	}
	configDir = filepath.Join(configDir, "memcached-gui")

	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create config dir: %w", err)
	}

	return &ConnectionService{
		configDir: configDir,
	}, nil
}

// LoadContexts loads saved contexts from the config file.
func (s *ConnectionService) LoadContexts() ([]Context, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	contexts, err := s.loadContextsLocked()
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}
	return contexts, nil
}

// SaveContext saves or updates a context.
func (s *ConnectionService) SaveContext(ctx Context) (Context, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ctx.ID == "" {
		ctx.ID = uuid.New().String()
	}

	contexts, err := s.loadContextsLocked()
	if err != nil {
		return ctx, err
	}

	// Update existing or append new
	found := false
	for i, c := range contexts {
		if c.ID == ctx.ID {
			contexts[i] = ctx
			found = true
			break
		}
	}
	if !found {
		contexts = append(contexts, ctx)
	}

	return ctx, s.saveContextsLocked(contexts)
}

// DeleteContext removes a context by ID.
func (s *ConnectionService) DeleteContext(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	contexts, err := s.loadContextsLocked()
	if err != nil {
		return err
	}

	filtered := make([]Context, 0, len(contexts))
	for _, c := range contexts {
		if c.ID != id {
			filtered = append(filtered, c)
		}
	}

	return s.saveContextsLocked(filtered)
}

// Connect establishes a connection to the memcached servers in the given context.
func (s *ConnectionService) Connect(ctxID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Disconnect existing connection
	if err := s.disconnectLocked(); err != nil {
		return fmt.Errorf("failed to close existing connection: %w", err)
	}

	contexts, err := s.loadContextsLocked()
	if err != nil {
		return err
	}

	var target *Context
	for i := range contexts {
		if contexts[i].ID == ctxID {
			target = &contexts[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("context not found: %s", ctxID)
	}

	if len(target.Servers) == 0 {
		return fmt.Errorf("no servers configured in context")
	}

	// Build comma-separated address string for cluster support
	addr := target.Servers[0].Address()
	for i := 1; i < len(target.Servers); i++ {
		addr += "," + target.Servers[i].Address()
	}

	client, err := memcached.New(addr)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Verify connection by requesting server version
	verifyCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := client.Version(verifyCtx); err != nil {
		return errors.Join(
			fmt.Errorf("failed to connect to %s: %w", addr, err),
			client.Close(),
		)
	}

	s.client = client
	s.connected = true
	s.activeCtx = target
	return nil
}

// Disconnect closes the current memcached connection.
func (s *ConnectionService) Disconnect() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.disconnectLocked()
}

func (s *ConnectionService) disconnectLocked() error {
	var err error
	if s.client != nil {
		err = s.client.Close()
		s.client = nil
	}
	s.connected = false
	s.activeCtx = nil
	return err
}

// IsConnected returns whether there's an active connection.
func (s *ConnectionService) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected
}

// GetClient returns the current memcached client (for operations service).
func (s *ConnectionService) GetClient() (memcached.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.client == nil {
		return nil, fmt.Errorf("not connected to any memcached server")
	}
	return s.client, nil
}

func (s *ConnectionService) loadContextsLocked() (contexts []Context, retErr error) {
	root, err := os.OpenRoot(s.configDir)
	if err != nil {
		return nil, err
	}
	defer func() {
		retErr = errors.Join(retErr, root.Close())
	}()

	data, err := root.ReadFile(configFileName)
	if err != nil {
		if os.IsNotExist(err) {
			return []Context{}, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, &contexts); err != nil {
		return nil, err
	}
	return contexts, nil
}

func (s *ConnectionService) saveContextsLocked(contexts []Context) (retErr error) {
	data, err := json.MarshalIndent(contexts, "", "  ")
	if err != nil {
		return err
	}

	root, err := os.OpenRoot(s.configDir)
	if err != nil {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, root.Close())
	}()

	if err := root.WriteFile(configFileName, data, 0o600); err != nil {
		return err
	}
	return root.Chmod(configFileName, 0o600)
}
