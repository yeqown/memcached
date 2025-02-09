package memcached

import (
	"time"

	"github.com/yeqown/memcached/conn"
	"github.com/yeqown/memcached/hash"
	"github.com/yeqown/memcached/protocol"
)

type normalTextProtocolClient interface {
	Set(key string, value any, expiry int64) error
	Get(key string, value any) error
}

type metaTextProtocolClient interface {
	MetaSet(key string)
	MetaGet(key string)
}

// Client represents a memcached client API set.
type Client interface {
	normalTextProtocolClient
	metaTextProtocolClient

	Version() (string, error)
}

type ClientOption func(*basicClient)

type basicClient struct {
	nodes         []*conn.Node
	hasher        hash.HashFunc
	timeout       time.Duration
	maxConns      int
	maxRetries    int
	retryInterval time.Duration
}

func (c *basicClient) Set(key string, value codecValue, expiry int64) error {
	data, err := value.Marshal()
	if err != nil {
		return err
	}

	node, err := c.getNode(key)
	if err != nil {
		return err
	}

	conn, err := node.GetConn()
	if err != nil {
		return err
	}
	defer node.PutConn(conn)

	cmd := &protocol.Command{
		Name:   protocol.CmdSet,
		Key:    key,
		Data:   data,
		Length: len(data),
		Expiry: expiry,
	}

	cmdBytes, err := protocol.BuildCommand(cmd)
	if err != nil {
		return err
	}

	if err := conn.Write(cmdBytes); err != nil {
		node.SetAvailable(false)
		return err
	}

	resp, err := conn.Read('\n')
	if err != nil {
		node.SetAvailable(false)
		return err
	}

	parsedResp, err := protocol.ParseResponse(resp)
	if err != nil {
		return err
	}

	if parsedResp.Status != protocol.StatusStored {
		return protocol.ErrServerError
	}

	return nil
}

func WithHasher(hasher hash.HashFunc) ClientOption {
	return func(c *basicClient) {
		c.hasher = hasher
	}
}

func WithMaxConns(maxConns int) ClientOption {
	return func(c *basicClient) {
		c.maxConns = maxConns
	}
}

func New(addrs []string, options ...ClientOption) *basicClient {
	c := &basicClient{
		timeout:  time.Second * 3,
		maxConns: 10,
	}

	for _, opt := range options {
		opt(c)
	}

	// 初始化节点
	c.nodes = make([]*conn.Node, len(addrs))
	for i, addr := range addrs {
		c.nodes[i] = conn.NewNode(addr, 1, c.maxConns, c.timeout)
	}

	// 默认使用 CRC32 哈希
	if c.hasher == nil {
		c.hasher = hash.NewCRC32()
	}

	return c
}

func (c *basicClient) getNode(key string) (*conn.Node, error) {
	if len(c.nodes) == 0 {
		return nil, protocol.ErrServerError
	}

	hash := c.hasher.Hash([]byte(key))
	index := int(hash % uint64(len(c.nodes)))
	node := c.nodes[index]

	if !node.IsAvailable() {
		return nil, protocol.ErrServerError
	}

	return node, nil
}

func (c *basicClient) Get(key string, value codecValue) error {
	node, err := c.getNode(key)
	if err != nil {
		return err
	}

	conn, err := node.GetConn()
	if err != nil {
		return err
	}
	defer node.PutConn(conn)

	cmd := &protocol.Command{
		Name: protocol.CmdGet,
		Key:  key,
	}

	cmdBytes, err := protocol.BuildCommand(cmd)
	if err != nil {
		return err
	}

	if err := conn.Write(cmdBytes); err != nil {
		return err
	}

	resp, err := conn.Read('\n')
	if err != nil {
		return err
	}

	parsedResp, err := protocol.ParseResponse(resp)
	if err != nil {
		return err
	}

	if parsedResp.Status == protocol.StatusNotFound {
		return protocol.ErrNotFound
	}

	return value.Unmarshal(parsedResp.Data)
}

func (c *basicClient) Delete(key string) error {
	node, err := c.getNode(key)
	if err != nil {
		return err
	}

	conn, err := node.GetConn()
	if err != nil {
		return err
	}
	defer node.PutConn(conn)

	cmd := &protocol.Command{
		Name: protocol.CmdDelete,
		Key:  key,
	}

	cmdBytes, err := protocol.BuildCommand(cmd)
	if err != nil {
		return err
	}

	if err := conn.Write(cmdBytes); err != nil {
		return err
	}

	resp, err := conn.Read('\n')
	if err != nil {
		return err
	}

	parsedResp, err := protocol.ParseResponse(resp)
	if err != nil {
		return err
	}

	if parsedResp.Status == protocol.StatusNotFound {
		return protocol.ErrNotFound
	}

	return nil
}
