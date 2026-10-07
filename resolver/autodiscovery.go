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

var (
	_ Resolver  = (*AutoDiscovery)(nil)
	_ io.Closer = (*AutoDiscovery)(nil)
)

// AutoDiscovery discovers AWS ElastiCache and Google Memorystore Memcached
// nodes using the shared ASCII config get cluster protocol. It reuses one TCP
// connection and returns its last successful configuration on timeout.
// Each instance belongs to one client, which closes it when no longer needed.
// The zero value uses the default refresh interval of one minute.
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
	initOnce        sync.Once
	ctx             context.Context
	cancelFn        context.CancelFunc
	resolveGate     chan struct{}

	// resolveGate serializes access to the connection and its cached result.
	target  string
	conn    net.Conn
	scanner *bufio.Scanner
	cached  ResolveResult
}

// NewAutoDiscovery creates a resolver that polls again refreshInterval after
// each attempt, including errors. Non-positive intervals select one minute.
// The client supplies each attempt's timeout through WithResolveTimeout.
// Only config get cluster is supported; legacy get discovery is not attempted.
func NewAutoDiscovery(refreshInterval time.Duration) *AutoDiscovery {
	return &AutoDiscovery{refreshInterval: refreshInterval}
}

func (r *AutoDiscovery) initialize() {
	r.initOnce.Do(func() {
		if r.refreshInterval <= 0 {
			r.refreshInterval = time.Minute
		}
		r.ctx, r.cancelFn = context.WithCancel(context.Background())
		r.resolveGate = make(chan struct{}, 1)
	})
}

// Resolve retrieves a complete cluster config and schedules another attempt.
// Target is a host:port address, optionally prefixed with tcp://, tcp4:// or
// tcp6://. IP addresses from the config take precedence over hostnames.
func (r *AutoDiscovery) Resolve(ctx context.Context, target string) (result ResolveResult, nextResolveAt *time.Time, err error) {
	r.initialize()
	defer func() {
		next := time.Now().Add(r.refreshInterval)
		nextResolveAt = &next
	}()
	if err := ctx.Err(); err != nil {
		return ResolveResult{}, nil, err
	}
	select {
	case <-ctx.Done():
		return ResolveResult{}, nil, ctx.Err()
	case <-r.ctx.Done():
		return ResolveResult{}, nil, net.ErrClosed
	case r.resolveGate <- struct{}{}:
	}
	defer func() { <-r.resolveGate }()
	if r.ctx.Err() != nil {
		return ResolveResult{}, nil, net.ErrClosed
	}
	network, address, err := resolveAddr(target)
	if err != nil {
		return ResolveResult{}, nil, err
	}
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return ResolveResult{}, nil, fmt.Errorf("%w: discovery requires TCP", ErrInvalidNetworkProtocol)
	}
	endpoint := network + "://" + address
	if endpoint != r.target {
		_ = r.closeConn()
		r.target, r.cached = endpoint, ResolveResult{}
	}
	attemptCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.ctx, cancel)
	defer func() { stop(); cancel() }()
	result, err = r.fetch(attemptCtx, network, address)
	if r.ctx.Err() != nil {
		_ = r.closeConn()
		return ResolveResult{}, nil, net.ErrClosed
	}
	if contextErr := ctx.Err(); contextErr != nil {
		err = contextErr
	}
	if err != nil {
		// A failed exchange may leave a partial command or response on the wire.
		// Preserve the exchange error even if closing the connection also fails.
		_ = r.closeConn()
		var netErr net.Error
		timedOut := errors.Is(err, context.DeadlineExceeded) || errors.As(err, &netErr) && netErr.Timeout()
		if timedOut && len(r.cached.Addrs) > 0 {
			return cloneConfig(r.cached), nil, nil
		}
		// Socket deadlines can fire before the context's cancellation timer.
		if timedOut {
			if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
				return ResolveResult{}, nil, context.DeadlineExceeded
			}
		}
		return ResolveResult{}, nil, err
	}
	if len(r.cached.Addrs) > 0 {
		if result.Generation < r.cached.Generation {
			return ResolveResult{}, nil, ErrStaleConfig
		}
		if result.Generation == r.cached.Generation {
			return cloneConfig(r.cached), nil, nil
		}
	}
	r.cached = result
	return cloneConfig(r.cached), nil, nil
}

func (r *AutoDiscovery) fetch(ctx context.Context, network, address string) (ResolveResult, error) {
	if r.conn == nil {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return ResolveResult{}, err
		}
		r.conn = conn
		r.scanner = newConfigScanner(conn)
	}
	conn := r.conn
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return ResolveResult{}, err
	}
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = conn.Close()
		close(interrupted)
	})
	defer func() {
		if !stop() {
			<-interrupted
		}
	}()
	if _, err := io.WriteString(conn, "config get cluster\r\n"); err != nil {
		return ResolveResult{}, err
	}
	return readConfigResponse(r.scanner)
}

// Close interrupts discovery and releases its persistent connection.
func (r *AutoDiscovery) Close() error {
	r.initialize()
	r.cancelFn()
	r.resolveGate <- struct{}{}
	defer func() { <-r.resolveGate }()
	return r.closeConn()
}

func (r *AutoDiscovery) closeConn() error {
	if r.conn == nil {
		return nil
	}
	conn := r.conn
	r.conn, r.scanner = nil, nil
	return conn.Close()
}

func cloneConfig(result ResolveResult) ResolveResult {
	cloned := ResolveResult{Generation: result.Generation, Addrs: make([]*Addr, len(result.Addrs))}
	for i, addr := range result.Addrs {
		cloned.Addrs[i] = addr.Clone()
	}
	return cloned
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

func newConfigScanner(input io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, maxConfigHeaderBytes), maxConfigBytes)
	return scanner
}

func readConfigResponse(scanner *bufio.Scanner) (ResolveResult, error) {
	// Read logical lines as the reference parser does. Keep the scanner for the
	// connection's lifetime so any prefetched bytes survive between responses.
	const maxResponseBytes = maxConfigBytes + maxConfigHeaderBytes + len("\r\nEND\r\n")
	bytesRead := 0
	nextLine := func() (string, error) {
		for scanner.Scan() {
			bytesRead += len(scanner.Bytes()) + 1
			if bytesRead > maxResponseBytes {
				return "", ErrMalformedResponse
			}
			if line := strings.TrimSpace(scanner.Text()); line != "" {
				return line, nil
			}
		}
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("%w: %w", ErrMalformedResponse, err)
		}
		return "", fmt.Errorf("%w: %w", ErrMalformedResponse, io.ErrUnexpectedEOF)
	}

	header, err := nextLine()
	if err != nil {
		return ResolveResult{}, err
	}
	if len(scanner.Bytes()) > maxConfigHeaderBytes {
		return ResolveResult{}, ErrMalformedResponse
	}
	if header == "END" {
		return ResolveResult{}, ErrNoClusterConfig
	}
	fields := strings.Fields(header)
	if len(fields) != 4 || fields[0] != "CONFIG" || fields[1] != "cluster" || fields[2] != "0" {
		return ResolveResult{}, ErrMalformedResponse
	}
	length, err := strconv.ParseUint(fields[3], 10, 32)
	if err != nil || length == 0 || length > maxConfigBytes {
		return ResolveResult{}, ErrMalformedResponse
	}
	var payload [3]string
	for i := range payload {
		payload[i], err = nextLine()
		if err != nil {
			return ResolveResult{}, err
		}
	}
	if payload[2] != "END" {
		return ResolveResult{}, ErrMalformedResponse
	}
	return parseConfigPayload(payload[0], payload[1])
}

func parseConfigPayload(versionLine, nodesLine string) (ResolveResult, error) {
	version, err := strconv.ParseUint(versionLine, 10, 64)
	if err != nil {
		return ResolveResult{}, ErrMalformedResponse
	}
	nodes := strings.Fields(nodesLine)
	if len(nodes) == 0 {
		return ResolveResult{}, ErrMalformedResponse
	}
	result := ResolveResult{Generation: version, Addrs: make([]*Addr, 0, len(nodes))}
	seen := make(map[AddrKey]struct{}, len(nodes))
	for _, node := range nodes {
		fields := strings.Split(node, "|")
		if len(fields) != 3 {
			return ResolveResult{}, ErrMalformedResponse
		}
		host, ip, port := fields[0], fields[1], fields[2]
		if ip != "" {
			if _, err := netip.ParseAddr(ip); err != nil {
				return ResolveResult{}, ErrMalformedResponse
			}
			host = ip
		}
		address, err := canonicalAddress("tcp", net.JoinHostPort(host, port))
		if err != nil {
			return ResolveResult{}, ErrMalformedResponse
		}
		// Discovery order does not express a node's routing priority.
		addr := NewAddr("tcp", address, 0)
		if _, duplicate := seen[addr.AddrKey]; duplicate {
			continue
		}
		seen[addr.AddrKey] = struct{}{}
		result.Addrs = append(result.Addrs, addr)
	}
	return result, nil
}
