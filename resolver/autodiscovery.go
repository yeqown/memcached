package resolver

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrStaleConfig reports a configuration older than one already observed for
// the same discovery endpoint.
var ErrStaleConfig = errors.New("stale cluster configuration")

var _ Resolver = (*AutoDiscovery)(nil)

// AutoDiscovery discovers AWS ElastiCache and Google Memorystore Memcached
// nodes using the shared ASCII config get cluster protocol. Each Resolve uses
// a separate TCP connection and honors the request's context.
//
// See [AWS Auto Discovery] and [Google Memorystore Auto Discovery] for service
// documentation, and [Memcache auto-discovery protocol] for the wire format.
// Response parsing follows [Google's reference parser].
//
// [AWS Auto Discovery]: https://docs.aws.amazon.com/AmazonElastiCache/latest/dg/AutoDiscovery.AddingToYourClientLibrary.html
// [Google Memorystore Auto Discovery]: https://cloud.google.com/memorystore/docs/memcached/use-auto-discovery
// [Memcache auto-discovery protocol]: https://docs.google.com/document/d/15V9tKuffWrcCVwDZRmRBOV1SDcuo6P8u05dddwZYxCo/edit
// [Google's reference parser]: https://github.com/google/gomemcache/blob/master/memcache/cluster_config_parser.go
type AutoDiscovery struct {
	refreshInterval time.Duration
	mu              sync.Mutex
	versions        map[string]uint64
}

// NewAutoDiscovery creates a resolver that polls again refreshInterval after
// each attempt, including errors. Non-positive intervals select one minute.
// The client supplies each attempt's timeout through WithResolveTimeout.
// Only config get cluster is supported; legacy get discovery is not attempted.
func NewAutoDiscovery(refreshInterval time.Duration) *AutoDiscovery {
	if refreshInterval <= 0 {
		refreshInterval = time.Minute
	}
	return &AutoDiscovery{refreshInterval: refreshInterval, versions: make(map[string]uint64)}
}

// Resolve retrieves a complete cluster config and schedules another attempt.
// Target is a host:port address, optionally prefixed with tcp://, tcp4:// or
// tcp6://. IP addresses from the config take precedence over hostnames.
func (r *AutoDiscovery) Resolve(ctx context.Context, target string) (result ResolveResult, nextResolveAt *time.Time, err error) {
	defer func() {
		interval := r.refreshInterval
		if interval <= 0 {
			interval = time.Minute
		}
		next := time.Now().Add(interval)
		nextResolveAt = &next
		if contextErr := ctx.Err(); contextErr != nil {
			result, err = ResolveResult{}, contextErr
		}
	}()
	if err := ctx.Err(); err != nil {
		return ResolveResult{}, nil, err
	}
	network, address, err := resolveAddr(target)
	if err != nil {
		return ResolveResult{}, nil, err
	}
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return ResolveResult{}, nil, fmt.Errorf("%w: discovery requires TCP", ErrInvalidNetworkProtocol)
	}
	endpoint := network + "://" + address
	cn, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return ResolveResult{}, nil, fmt.Errorf("dial discovery endpoint: %w", err)
	}
	defer func() { _ = cn.Close() }()
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = cn.Close()
		close(interrupted)
	})
	defer func() {
		if !stop() {
			<-interrupted
		}
	}()
	if _, err := io.WriteString(cn, "config get cluster\r\n"); err != nil {
		return ResolveResult{}, nil, fmt.Errorf("write discovery command: %w", err)
	}
	result, err = readConfigResponse(cn)
	if err != nil {
		return ResolveResult{}, nil, err
	}

	version := result.Generation

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.versions == nil {
		r.versions = make(map[string]uint64)
	}
	if previous, ok := r.versions[endpoint]; ok && version < previous {
		return ResolveResult{}, nil, fmt.Errorf("%w: version %d is older than %d", ErrStaleConfig, version, previous)
	}
	r.versions[endpoint] = version
	return result, nil, nil
}

const (
	maxConfigBytes       = 1 << 20
	maxConfigHeaderBytes = 4 << 10
)

var (
	// ErrMalformedResponse reports an invalid config get cluster response.
	ErrMalformedResponse = errors.New("malformed cluster config response")
	// ErrNoClusterConfig reports an END response with no cluster configuration.
	ErrNoClusterConfig = errors.New("no cluster configuration")
)

func readConfigResponse(input io.Reader) (ResolveResult, error) {
	// Like the reference parser, read logical lines instead of framing the body
	// by the advertised byte count. Bound both the stream and individual lines.
	maxResponseBytes := int64(maxConfigBytes + maxConfigHeaderBytes + len("\r\nEND\r\n"))
	// Read one extra byte to distinguish a complete response at the limit from
	// a larger response truncated into a valid-looking final token.
	limited := &io.LimitedReader{R: input, N: maxResponseBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, maxConfigHeaderBytes), maxConfigBytes)
	nextLine := func() (string, error) {
		for scanner.Scan() {
			if limited.N == 0 {
				break
			}
			if line := strings.TrimSpace(scanner.Text()); line != "" {
				return line, nil
			}
		}
		if limited.N == 0 {
			return "", fmt.Errorf("config response exceeds %d bytes", maxResponseBytes)
		}
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", io.ErrUnexpectedEOF
	}

	header, err := nextLine()
	if err != nil {
		return ResolveResult{}, fmt.Errorf("%w: config header: %v", ErrMalformedResponse, err)
	}
	if len(scanner.Bytes()) > maxConfigHeaderBytes {
		return ResolveResult{}, fmt.Errorf("%w: config header too large", ErrMalformedResponse)
	}
	if header == "END" {
		return ResolveResult{}, ErrNoClusterConfig
	}
	fields := strings.Fields(header)
	if len(fields) != 4 || fields[0] != "CONFIG" || fields[1] != "cluster" || fields[2] != "0" {
		return ResolveResult{}, fmt.Errorf("%w: unexpected config header", ErrMalformedResponse)
	}
	length, err := strconv.ParseUint(fields[3], 10, 32)
	if err != nil || length == 0 || length > maxConfigBytes {
		return ResolveResult{}, fmt.Errorf("%w: invalid config size", ErrMalformedResponse)
	}
	versionLine, err := nextLine()
	if err != nil {
		return ResolveResult{}, fmt.Errorf("%w: config version: %v", ErrMalformedResponse, err)
	}
	nodesLine, err := nextLine()
	if err != nil {
		return ResolveResult{}, fmt.Errorf("%w: config nodes: %v", ErrMalformedResponse, err)
	}
	// Discovery endpoints may keep the connection open after a complete reply.
	// Stop at END rather than waiting for EOF as the reference parser does.
	end, err := nextLine()
	if err != nil {
		return ResolveResult{}, fmt.Errorf("%w: missing config END: %v", ErrMalformedResponse, err)
	}
	if end != "END" {
		return ResolveResult{}, fmt.Errorf("%w: missing config END", ErrMalformedResponse)
	}
	return parseConfigPayload(versionLine, nodesLine)
}

func parseConfigPayload(versionLine, nodesLine string) (ResolveResult, error) {
	version, err := strconv.ParseUint(versionLine, 10, 64)
	if err != nil {
		return ResolveResult{}, fmt.Errorf("%w: invalid config version", ErrMalformedResponse)
	}
	nodes := strings.Fields(nodesLine)
	if len(nodes) == 0 {
		return ResolveResult{}, fmt.Errorf("%w: empty cluster", ErrMalformedResponse)
	}
	result := ResolveResult{Generation: version, Addrs: make([]*Addr, 0, len(nodes))}
	for _, node := range nodes {
		fields := strings.Split(node, "|")
		if len(fields) != 3 {
			return ResolveResult{}, fmt.Errorf("%w: invalid node fields", ErrMalformedResponse)
		}
		host, ip, port := fields[0], fields[1], fields[2]
		if ip != "" {
			if _, err := netip.ParseAddr(ip); err != nil {
				return ResolveResult{}, fmt.Errorf("%w: invalid node IP", ErrMalformedResponse)
			}
			host = ip
		}
		address, err := canonicalAddress("tcp", net.JoinHostPort(host, port))
		if err != nil {
			return ResolveResult{}, fmt.Errorf("%w: invalid node address: %v", ErrMalformedResponse, err)
		}
		// Discovery order is not a stable routing attribute. A constant priority
		// keeps legacy rendezvous scores stable when the server reorders nodes.
		result.Addrs = append(result.Addrs, NewAddr("tcp", address, 0))
	}
	return result, nil
}
