package main

import (
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/yeqown/memcached"
)

// Context represents a memcached instance group configuration
type Context struct {
	Name      string       `json:"name"`
	Servers   string       `json:"servers"`
	Config    clientConfig `json:"config"`
	CreatedAt time.Time    `json:"created_at"`
	LastUsed  time.Time    `json:"last_used"`
}

// clientConfig contains connection-related settings
type clientConfig struct {
	PoolSize     int           `json:"pool_size"`
	DialTimeout  time.Duration `json:"dial_timeout"`
	ReadTimeout  time.Duration `json:"read_timeout"`
	WriteTimeout time.Duration `json:"write_timeout"`
	HashStrategy string        `json:"hash_strategy"` // only crc32, murmur3, rendezvous(default)
}

// DefaultConfig returns a ConnectionConfig with default values
func defaultConfig(hashStrategy *string) clientConfig {
	hash := "rendezvous"
	if hashStrategy != nil {
		if lo.Contains([]string{"crc32", "murmur3", "rendezvous"}, *hashStrategy) {
			hash = *hashStrategy
		} else {
			logger.Debugf("hash strategy not found in config, using default: %s", hash)
		}
	}

	return clientConfig{
		PoolSize:     10,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		HashStrategy: hash,
	}
}

func createClient(ctx *Context) (memcached.Client, error) {
	var builder memcached.Builder = memcached.NewCr32HashPickBuilder()
	switch ctx.Config.HashStrategy {
	case "rendezvous":
		builder = memcached.NewRendezvousHashPickBuilder(magicSeed)
	case "murmur3":
		builder = memcached.NewMurmur3HashPickBuilder(magicSeed)
	}

	_uniqueServers := make([]string, 0, 4)
	for _, server := range strings.Split(ctx.Servers, ",") {
		if lo.Contains(_uniqueServers, strings.TrimSpace(server)) {
			continue
		}

		_uniqueServers = append(_uniqueServers, strings.TrimSpace(server))
	}
	uniqServers := strings.Join(_uniqueServers, ",")

	client, err := memcached.New(
		uniqServers,
		memcached.WithPickBuilder(builder),
		memcached.WithMaxConns(ctx.Config.PoolSize),
		memcached.WithDialTimeout(ctx.Config.DialTimeout),
		memcached.WithReadTimeout(ctx.Config.ReadTimeout),
		memcached.WithWriteTimeout(ctx.Config.WriteTimeout),
	)
	if err != nil {
		return nil, err
	}

	return client, nil
}
