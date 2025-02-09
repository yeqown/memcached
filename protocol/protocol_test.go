package protocol

import (
	"bytes"
	"reflect"
	"testing"
)

func TestBuildCommand(t *testing.T) {
	tests := []struct {
		name    string
		cmd     *Command
		want    []byte
		wantErr bool
	}{
		{
			name: "set command without cas",
			cmd: &Command{
				Name:   CmdSet,
				Key:    "test-key",
				Flags:  1,
				Expiry: 3600,
				Length: 5,
				Data:   []byte("hello"),
			},
			want:    []byte("set test-key 1 3600 5\r\nhello\r\n"),
			wantErr: false,
		},
		{
			name: "set command with cas",
			cmd: &Command{
				Name:   CmdSet,
				Key:    "test-key",
				Flags:  1,
				Expiry: 3600,
				Length: 5,
				Data:   []byte("hello"),
				Cas:    12345,
			},
			want:    []byte("set test-key 1 3600 5 12345\r\nhello\r\n"),
			wantErr: false,
		},
		{
			name: "get command",
			cmd: &Command{
				Name: CmdGet,
				Key:  "test-key",
			},
			want:    []byte("get test-key\r\n"),
			wantErr: false,
		},
		{
			name: "gets command",
			cmd: &Command{
				Name: CmdGets,
				Key:  "test-key",
			},
			want:    []byte("gets test-key\r\n"),
			wantErr: false,
		},
		{
			name: "delete command",
			cmd: &Command{
				Name: CmdDelete,
				Key:  "test-key",
			},
			want:    []byte("delete test-key\r\n"),
			wantErr: false,
		},
		{
			name: "incr command",
			cmd: &Command{
				Name:   CmdIncr,
				Key:    "counter",
				Length: 1,
			},
			want:    []byte("incr counter 1\r\n"),
			wantErr: false,
		},
		{
			name: "decr command",
			cmd: &Command{
				Name:   CmdDecr,
				Key:    "counter",
				Length: 1,
			},
			want:    []byte("decr counter 1\r\n"),
			wantErr: false,
		},
		{
			name: "touch command",
			cmd: &Command{
				Name:   CmdTouch,
				Key:    "test-key",
				Expiry: 3600,
			},
			want:    []byte("touch test-key 3600\r\n"),
			wantErr: false,
		},
		{
			name: "version command",
			cmd: &Command{
				Name: CmdVersion,
			},
			want:    []byte("version\r\n"),
			wantErr: false,
		},
		{
			name: "unsupported command",
			cmd: &Command{
				Name: "unknown",
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildCommand(tt.cmd)
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !bytes.Equal(got, tt.want) {
				t.Errorf("BuildCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseResponse(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    *Response
		wantErr bool
	}{
		{
			name:    "empty response",
			data:    []byte{},
			want:    nil,
			wantErr: true,
		},
		{
			name: "STORED response",
			data: []byte("STORED\r\n"),
			want: &Response{
				Status: StatusStored,
			},
			wantErr: false,
		},
		{
			name: "NOT_STORED response",
			data: []byte("NOT_STORED\r\n"),
			want: &Response{
				Status: StatusNotStored,
			},
			wantErr: false,
		},
		{
			name: "EXISTS response",
			data: []byte("EXISTS\r\n"),
			want: &Response{
				Status: StatusExists,
			},
			wantErr: false,
		},
		{
			name: "NOT_FOUND response",
			data: []byte("NOT_FOUND\r\n"),
			want: &Response{
				Status: StatusNotFound,
			},
			wantErr: false,
		},
		{
			name: "ERROR response without message",
			data: []byte("ERROR\r\n"),
			want: &Response{
				Status: StatusError,
			},
			wantErr: false,
		},
		{
			name: "ERROR response with message",
			data: []byte("ERROR\r\nOut of memory\r\n"),
			want: &Response{
				Status:  StatusError,
				Message: "Out of memory",
			},
			wantErr: false,
		},
		{
			name: "VALUE response",
			data: []byte("VALUE mykey 0 5 123\r\nhello\r\nEND\r\n"),
			want: &Response{
				Status: "VALUE",
				Data:   []byte("hello"),
				Flags:  0,
				Cas:    123,
			},
			wantErr: false,
		},
		{
			name: "VALUE response without cas",
			data: []byte("VALUE mykey 0 5\r\nhello\r\nEND\r\n"),
			want: &Response{
				Status: "VALUE",
				Data:   []byte("hello"),
				Flags:  0,
			},
			wantErr: false,
		},
		{
			name:    "invalid VALUE response",
			data:    []byte("VALUE\r\n"),
			want:    nil,
			wantErr: true,
		},
		{
			name:    "unknown response",
			data:    []byte("UNKNOWN\r\n"),
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseResponse(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseResponse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseResponse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parseValueResponse(t *testing.T) {
	tests := []struct {
		name    string
		lines   [][]byte
		want    *Response
		wantErr bool
	}{
		{
			name: "正常响应不带CAS",
			lines: [][]byte{
				[]byte("VALUE mykey 123 3"),
				[]byte("foo"),
				[]byte(""),
			},
			want: &Response{
				Status: "VALUE",
				Data:   []byte("foo"),
				Flags:  123,
			},
			wantErr: false,
		},
		{
			name: "正常响应带CAS",
			lines: [][]byte{
				[]byte("VALUE mykey 456 3 789"),
				[]byte("bar"),
				[]byte(""),
			},
			want: &Response{
				Status: "VALUE",
				Data:   []byte("bar"),
				Flags:  456,
				Cas:    789,
			},
			wantErr: false,
		},
		{
			name: "行数不足",
			lines: [][]byte{
				[]byte("VALUE mykey 123 3"),
				[]byte("foo"),
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "VALUE行格式错误",
			lines: [][]byte{
				[]byte("VALUE mykey"),
				[]byte("foo"),
				[]byte(""),
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "flags不是有效数字",
			lines: [][]byte{
				[]byte("VALUE mykey abc 3"),
				[]byte("foo"),
				[]byte(""),
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "cas不是有效数字",
			lines: [][]byte{
				[]byte("VALUE mykey 123 3 abc"),
				[]byte("foo"),
				[]byte(""),
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseValueResponse(tt.lines)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseValueResponse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseValueResponse() = %v, want %v", got, tt.want)
			}
		})
	}
}
