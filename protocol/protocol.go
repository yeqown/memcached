package protocol

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Command 表示 Memcached 命令
type Command struct {
	Name   string
	Key    string
	Flags  uint32
	Expiry int64
	Length int
	Data   []byte
	Cas    uint64
}

// Response 表示 Memcached 响应
// Response 结构中添加 MultiData 字段
type Response struct {
    Status    string
    Data      []byte
    MultiData map[string][]byte // 用于多键值响应
    Cas       uint64
    Flags     uint32
    Message   string
    Value     uint64
}

// 定义常用命令
const (
	CmdSet     = "set"
	CmdGet     = "get"
	CmdGets    = "gets"
	CmdDelete  = "delete"
	CmdIncr    = "incr"
	CmdDecr    = "decr"
	CmdTouch   = "touch"
	CmdVersion = "version"
)

// 定义响应状态
const (
	StatusStored    = "STORED"
	StatusNotStored = "NOT_STORED"
	StatusExists    = "EXISTS"
	StatusNotFound  = "NOT_FOUND"
	StatusError     = "ERROR"
)

// BuildCommand 将命令转换为字节流
func BuildCommand(cmd *Command) ([]byte, error) {
	switch cmd.Name {
	case CmdSet:
		return []byte(fmt.Sprintf("%s %s %d %d %d%s\r\n%s\r\n",
			cmd.Name, cmd.Key, cmd.Flags, cmd.Expiry, cmd.Length,
			formatCAS(cmd.Cas), cmd.Data)), nil
	case CmdGet, CmdGets:
		return []byte(fmt.Sprintf("%s %s\r\n", cmd.Name, cmd.Key)), nil
	case CmdDelete:
		return []byte(fmt.Sprintf("delete %s\r\n", cmd.Key)), nil
	case CmdIncr, CmdDecr:
		return []byte(fmt.Sprintf("%s %s %d\r\n", cmd.Name, cmd.Key, cmd.Length)), nil
	case CmdTouch:
		return []byte(fmt.Sprintf("touch %s %d\r\n", cmd.Key, cmd.Expiry)), nil
	case CmdVersion:
		return []byte("version\r\n"), nil
	default:
		return nil, fmt.Errorf("unsupported command: %s", cmd.Name)
	}
}

// ParseResponse 解析响应数据
func ParseResponse(data []byte) (*Response, error) {
	resp := &Response{}
	lines := bytes.Split(data, []byte("\r\n"))
	if len(lines) == 0 {
		return nil, errors.New("empty response")
	}

	firstLine := string(lines[0])
	switch {
	case firstLine == StatusStored:
		resp.Status = StatusStored
	case firstLine == StatusNotStored:
		resp.Status = StatusNotStored
	case firstLine == StatusExists:
		resp.Status = StatusExists
	case firstLine == StatusNotFound:
		resp.Status = StatusNotFound
	case firstLine == StatusError:
		resp.Status = StatusError
		if len(lines) > 1 {
			resp.Message = string(lines[1])
		}
	case strings.HasPrefix(firstLine, "VALUE"):
		// 寻找 END 标记
		endIndex := -1
		for i, line := range lines {
			if string(line) == "END" {
				endIndex = i
				break
			}
		}
		if endIndex == -1 {
			return nil, errors.New("missing END marker")
		}
		return parseValueResponse(lines[:endIndex+1])
	case strings.HasPrefix(firstLine, "VERSION"):
		resp.Status = "VERSION"
		resp.Message = firstLine[8:] // Skip "VERSION "
		return resp, nil
	default:
		// Try to parse as numeric response (for incr/decr)
		if val, err := strconv.ParseUint(firstLine, 10, 64); err == nil {
			resp.Status = "NUMERIC"
			resp.Value = val
			return resp, nil
		}
		return nil, fmt.Errorf("unknown response: %s", firstLine)
	}
	return resp, nil
}

func parseValueResponse(lines [][]byte) (*Response, error) {
	if len(lines) < 3 {
		return nil, errors.New("invalid VALUE response")
	}

	resp := &Response{}
	parts := bytes.Fields(lines[0])
	if len(parts) < 4 {
		return nil, errors.New("invalid VALUE line")
	}

	flags, err := strconv.ParseUint(string(parts[2]), 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid flags: %v", err)
	}
	resp.Flags = uint32(flags)

	length, err := strconv.Atoi(string(parts[3]))
	if err != nil {
		return nil, fmt.Errorf("invalid length: %v", err)
	}

	if len(parts) > 4 {
		cas, err := strconv.ParseUint(string(parts[4]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid cas: %v", err)
		}
		resp.Cas = cas
	}

	// 验证数据长度
	if len(lines[1]) != length {
		return nil, fmt.Errorf("data length mismatch: expected %d, got %d", length, len(lines[1]))
	}

	resp.Data = lines[1]
	resp.Status = "VALUE"
	return resp, nil
}

func formatCAS(cas uint64) string {
	if cas > 0 {
		return fmt.Sprintf(" %d", cas)
	}
	return ""
}
