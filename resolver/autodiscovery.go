package resolver

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	pkgerrors "github.com/pkg/errors"
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
// Create instances with NewAutoDiscovery and keep one fixed target per client.
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
	ctx             context.Context
	cancelFn        context.CancelFunc

	// Resolve calls are serialized by the client; Close may run concurrently.
	connMu sync.Mutex
	conn   net.Conn
	cached ResolveResult
}

// NewAutoDiscovery creates a resolver that polls again refreshInterval after
// each attempt, including errors. Non-positive intervals select one minute.
// The client supplies each attempt's timeout through WithResolveTimeout.
// Only config get cluster is supported; legacy get discovery is not attempted.
func NewAutoDiscovery(refreshInterval time.Duration) *AutoDiscovery {
	if refreshInterval <= 0 {
		refreshInterval = time.Minute
	}

	ctx, cancelFn := context.WithCancel(context.Background())

	return &AutoDiscovery{
		refreshInterval: refreshInterval,
		ctx:             ctx,
		cancelFn:        cancelFn,
		conn:            nil,
		cached:          ResolveResult{},
	}
}

var emptyResolveResult = ResolveResult{}

// Resolve retrieves a complete cluster config and schedules another attempt.
// Target is a host:port address, optionally prefixed with tcp://, tcp4:// or
// tcp6://. IP addresses from the config take precedence over hostnames.
func (r *AutoDiscovery) Resolve(ctx context.Context, target string) (result ResolveResult, nextResolveAt *time.Time, err error) {
	next := time.Now().Add(r.refreshInterval)
	nextResolveAt = &next

	select {
	case <-ctx.Done():
		return emptyResolveResult, nextResolveAt, ctx.Err()
	case <-r.ctx.Done():
		return emptyResolveResult, nextResolveAt, net.ErrClosed
	default:
	}

	attemptCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.ctx, cancel)
	defer func() { stop(); cancel() }()

	conn, err := r.getConn(attemptCtx, target)
	if err != nil {
		if r.ctx.Err() != nil {
			return emptyResolveResult, nextResolveAt, net.ErrClosed
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return emptyResolveResult, nextResolveAt, contextErr
		}
		return emptyResolveResult, nextResolveAt, pkgerrors.Wrap(err, "getting discovery connection failed")
	}

	result, err = r.fetch(attemptCtx, conn)
	if r.ctx.Err() != nil {
		_ = r.closeConn()
		return emptyResolveResult, nextResolveAt, net.ErrClosed
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
			return r.cached.Clone(), nextResolveAt, nil
		}
		// Socket deadlines can fire before the context's cancellation timer.
		if timedOut {
			if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
				return emptyResolveResult, nextResolveAt, context.DeadlineExceeded
			}
		}

		return emptyResolveResult, nextResolveAt, err
	}

	if len(r.cached.Addrs) > 0 {
		if result.Generation < r.cached.Generation {
			return emptyResolveResult, nextResolveAt, ErrStaleConfig
		}
		if result.Generation == r.cached.Generation {
			return r.cached.Clone(), nextResolveAt, nil
		}
	}

	r.cached = result

	return r.cached.Clone(), nextResolveAt, nil
}

func (r *AutoDiscovery) getConn(ctx context.Context, target string) (net.Conn, error) {
	r.connMu.Lock()
	defer r.connMu.Unlock()
	if r.ctx.Err() != nil {
		return nil, net.ErrClosed
	}
	if r.conn != nil {
		return r.conn, nil
	}

	network, address, err := resolveAddr(target)
	if err != nil {
		return nil, err
	}
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("%w: discovery requires TCP", ErrInvalidNetworkProtocol)
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return nil, pkgerrors.Wrap(err, "dialing discovery endpoint failed")
	}

	r.conn = conn
	return conn, nil
}

// Close interrupts discovery and releases its persistent connection.
func (r *AutoDiscovery) Close() error {
	r.cancelFn()
	return r.closeConn()
}

func (r *AutoDiscovery) closeConn() error {
	r.connMu.Lock()
	conn := r.conn
	r.conn = nil
	r.connMu.Unlock()
	if conn == nil {
		return nil
	}
	err := conn.Close()
	// A cancellation callback may already have closed this socket.
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func (r *AutoDiscovery) fetch(ctx context.Context, conn net.Conn) (ResolveResult, error) {
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return emptyResolveResult, err
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

	rw := bufio.NewWriter(conn)
	if _, err := rw.WriteString("config get cluster\r\n"); err != nil {
		return ResolveResult{}, err
	}
	if err := rw.Flush(); err != nil {
		return ResolveResult{}, err
	}

	rr := bufio.NewReader(conn)
	return readConfigResponse(ctx, rr)
}

var (
	// ErrMalformedResponse reports an invalid config get cluster response.
	ErrMalformedResponse = errors.New("malformed cluster config response")
	// ErrNoClusterConfig reports an END response with no cluster configuration.
	ErrNoClusterConfig = errors.New("no cluster configuration")
)

// readConfigResponse reads one complete CONFIG response, stopping at END.
func readConfigResponse(ctx context.Context, r *bufio.Reader) (ResolveResult, error) {
	scanner := bufio.NewScanner(r)
	result := ResolveResult{}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return emptyResolveResult, pkgerrors.Wrap(err, "context canceled while reading config response")
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == "END" {
			if len(result.Addrs) == 0 {
				return emptyResolveResult, ErrNoClusterConfig
			}
			return result, nil
		}

		fields := strings.Fields(line)
		if len(fields) != 4 || fields[0] != "CONFIG" || fields[1] != "cluster" || fields[2] != "0" {
			return emptyResolveResult, ErrMalformedResponse
		}
		if _, err := strconv.ParseUint(fields[3], 10, 64); err != nil {
			return emptyResolveResult, ErrMalformedResponse
		}

		if !scanner.Scan() {
			break
		}
		generation, err := strconv.ParseUint(strings.TrimSpace(scanner.Text()), 10, 64)
		if err != nil {
			return emptyResolveResult, fmt.Errorf("%w: parsing generation failed: %w", ErrMalformedResponse, err)
		}
		result.Generation = generation

		if !scanner.Scan() {
			break
		}
		nodes := strings.Fields(scanner.Text())
		if len(nodes) == 0 {
			return emptyResolveResult, ErrMalformedResponse
		}
		for _, node := range nodes {
			nodeHostPort := strings.Split(node, "|")
			if len(nodeHostPort) != 3 {
				return emptyResolveResult, fmt.Errorf("%w: host address (%s) not in expected format", ErrMalformedResponse, node)
			}
			nodePort, err := strconv.ParseUint(nodeHostPort[2], 10, 16)
			if err != nil {
				return emptyResolveResult, fmt.Errorf("%w: parsing node port failed: %w", ErrMalformedResponse, err)
			}
			host := nodeHostPort[1]
			if host == "" {
				host = nodeHostPort[0]
			}
			address, err := canonicalAddress("tcp", net.JoinHostPort(host, strconv.FormatUint(nodePort, 10)))
			if err != nil {
				return emptyResolveResult, fmt.Errorf("%w: %w", ErrMalformedResponse, err)
			}
			result.Addrs = append(result.Addrs, NewAddr("tcp", address, 0))
		}
	}
	if err := ctx.Err(); err != nil {
		return emptyResolveResult, pkgerrors.Wrap(err, "context canceled while reading config response")
	}
	if err := scanner.Err(); err != nil {
		return emptyResolveResult, pkgerrors.Wrap(err, "reading config response failed")
	}
	return emptyResolveResult, fmt.Errorf("%w: %w", ErrMalformedResponse, io.ErrUnexpectedEOF)
}
