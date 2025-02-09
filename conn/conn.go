package conn

import (
    "bufio"
    "net"
    "time"
)

type Conn struct {
    raw    net.Conn
    reader *bufio.Reader
    writer *bufio.Writer
}

func NewConn(addr string, timeout time.Duration) (*Conn, error) {
    raw, err := net.DialTimeout("tcp", addr, timeout)
    if err != nil {
        return nil, err
    }

    return &Conn{
        raw:    raw,
        reader: bufio.NewReader(raw),
        writer: bufio.NewWriter(raw),
    }, nil
}

func (c *Conn) Close() error {
    return c.raw.Close()
}

func (c *Conn) Write(data []byte) error {
    _, err := c.writer.Write(data)
    if err != nil {
        return err
    }
    return c.writer.Flush()
}

func (c *Conn) Read(delim byte) ([]byte, error) {
    return c.reader.ReadBytes(delim)
}