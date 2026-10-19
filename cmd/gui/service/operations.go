package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	memcached "github.com/yeqown/memcached"
	memcodec "github.com/yeqown/memcached/codec"
)

// OperationService exposes memcached operations to the frontend.
type OperationService struct {
	conn *ConnectionService
}

// NewOperationService creates a new OperationService.
func NewOperationService(conn *ConnectionService) *OperationService {
	return &OperationService{conn: conn}
}

const opTimeout = 10 * time.Second

func (s *OperationService) withTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), opTimeout)
}

func classifyValueKind(value []byte) string {
	if json.Valid(value) {
		return "json"
	}

	return "text"
}

func normalizeMemcachedKey(key string) (string, error) {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return "", fmt.Errorf("key cannot be empty")
	}

	if strings.IndexFunc(trimmed, unicode.IsSpace) >= 0 {
		return "", fmt.Errorf("key cannot contain whitespace")
	}

	return trimmed, nil
}

// Get retrieves a value by key.
func (s *OperationService) Get(key string) *OperationResult {
	normalizedKey, err := normalizeMemcachedKey(key)
	if err != nil {
		return &OperationResult{Success: false, Error: err.Error()}
	}

	client, err := s.conn.GetClient()
	if err != nil {
		return &OperationResult{Success: false, Error: err.Error()}
	}

	ctx, cancel := s.withTimeout()
	defer cancel()

	item, err := client.MetaGet(ctx, []byte(normalizedKey),
		memcached.MetaGetFlagReturnValue(),
		memcached.MetaGetFlagReturnTTL(),
		memcached.MetaGetFlagReturnLastAccessedTime(),
		memcached.MetaGetFlagReturnCAS(),
		memcached.MetaGetFlagReturnClientFlags(),
		memcached.MetaGetFlagReturnSize(),
		memcached.MetaGetFlagReturnHitBefore(),
	)
	if err != nil {
		return &OperationResult{Success: false, Error: err.Error()}
	}

	value := string(item.Value)
	return &OperationResult{
		Success:          true,
		Data:             value,
		Key:              string(item.Key),
		Value:            value,
		TTL:              item.TTL,
		LastAccessedTime: item.LastAccessedTime,
		CAS:              item.CAS,
		Flags:            memcodec.AppFlags(item.Flags),
		Size:             item.Size,
		HitBefore:        item.HitBefore,
		Opaque:           item.Opaque,
		ValueKind:        classifyValueKind(item.Value),
	}
}

// Set stores a key-value pair. expiry is in seconds (0 = no expiry).
func (s *OperationService) Set(key, value string, flags uint32, expirySeconds int) error {
	normalizedKey, err := normalizeMemcachedKey(key)
	if err != nil {
		return err
	}

	client, err := s.conn.GetClient()
	if err != nil {
		return err
	}

	ctx, cancel := s.withTimeout()
	defer cancel()

	return client.Set(ctx, normalizedKey, []byte(value), flags, time.Duration(expirySeconds)*time.Second)
}

// Delete removes a key.
func (s *OperationService) Delete(key string) error {
	normalizedKey, err := normalizeMemcachedKey(key)
	if err != nil {
		return err
	}

	client, err := s.conn.GetClient()
	if err != nil {
		return err
	}

	ctx, cancel := s.withTimeout()
	defer cancel()

	return client.Delete(ctx, normalizedKey)
}

// Stats returns server statistics as a JSON string.
func (s *OperationService) Stats() (*OperationResult, error) {
	client, err := s.conn.GetClient()
	if err != nil {
		return nil, err
	}

	ctx, cancel := s.withTimeout()
	defer cancel()

	stat, err := client.Stats(ctx)
	if err != nil {
		return nil, err
	}

	data, err := json.MarshalIndent(stat, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal stats: %w", err)
	}

	return &OperationResult{Success: true, Data: string(data)}, nil
}

// FlushAll flushes all data from the server.
func (s *OperationService) FlushAll() error {
	client, err := s.conn.GetClient()
	if err != nil {
		return err
	}

	ctx, cancel := s.withTimeout()
	defer cancel()

	return client.FlushAll(ctx)
}

// Incr increments a numeric key.
func (s *OperationService) Incr(key string, delta uint64) (*OperationResult, error) {
	normalizedKey, err := normalizeMemcachedKey(key)
	if err != nil {
		return nil, err
	}

	client, err := s.conn.GetClient()
	if err != nil {
		return nil, err
	}

	ctx, cancel := s.withTimeout()
	defer cancel()

	result, err := client.Incr(ctx, normalizedKey, delta)
	if err != nil {
		return nil, err
	}

	return &OperationResult{
		Success: true,
		Data:    fmt.Sprintf("%d", result),
	}, nil
}

// Decr decrements a numeric key.
func (s *OperationService) Decr(key string, delta uint64) (*OperationResult, error) {
	normalizedKey, err := normalizeMemcachedKey(key)
	if err != nil {
		return nil, err
	}

	client, err := s.conn.GetClient()
	if err != nil {
		return nil, err
	}

	ctx, cancel := s.withTimeout()
	defer cancel()

	result, err := client.Decr(ctx, normalizedKey, delta)
	if err != nil {
		return nil, err
	}

	return &OperationResult{
		Success: true,
		Data:    fmt.Sprintf("%d", result),
	}, nil
}

// Version returns the server version.
func (s *OperationService) Version() (string, error) {
	client, err := s.conn.GetClient()
	if err != nil {
		return "", err
	}

	ctx, cancel := s.withTimeout()
	defer cancel()

	return client.Version(ctx)
}
