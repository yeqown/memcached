package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/yeqown/memcached"
)

type replCommander struct {
	cm      *contextManager
	timeout time.Duration
	out     io.Writer
}

func newREPLCommander(manager *contextManager, timeout time.Duration) (*replCommander, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	return &replCommander{
		cm:      manager,
		timeout: timeout,
		out:     io.Discard,
	}, nil
}

var replCommands = []string{
	"use", "list", "current",
	"get", "gets", "set", "delete", "incr", "decr", "touch",
	"version", "help", "exit", "quit",
}

// completeREPLCommands returns command suggestions matching the first-token prefix.
func completeREPLCommands(line string) []string {
	fields := strings.Fields(line)
	if len(fields) == 0 || strings.HasSuffix(line, " ") {
		return nil
	}
	if len(fields) != 1 {
		return nil
	}

	prefix := strings.ToLower(fields[0])
	matches := make([]string, 0, len(replCommands))
	for _, cmd := range replCommands {
		if strings.HasPrefix(cmd, prefix) {
			matches = append(matches, cmd)
		}
	}
	return matches
}

type commandResult struct {
	output string
	quit   bool
}

// executeCommand runs a REPL command and captures its output.
func (r *replCommander) executeCommand(line string) commandResult {
	line = strings.TrimSpace(line)
	if line == "" {
		return commandResult{}
	}

	var buf bytes.Buffer
	prev := r.out
	r.out = &buf
	defer func() { r.out = prev }()

	args := strings.Fields(line)
	cmd := args[0]

	var err error
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()

	quit := false
	switch cmd {
	case "use":
		err = r.handleUse(ctx, args)
	case "list":
		err = r.handleList(ctx)
	case "current":
		err = r.handleCurrent(ctx)
	case "get":
		err = r.handleGet(ctx, args)
	case "gets":
		err = r.handleMGet(ctx, args)
	case "set":
		err = r.handleSet(ctx, args)
	case "delete":
		err = r.handleDelete(ctx, args)
	case "incr":
		err = r.handleIncr(ctx, args)
	case "decr":
		err = r.handleDecr(ctx, args)
	case "touch":
		err = r.handleTouch(ctx, args)
	case "version":
		err = r.handleVersion(ctx)
	case "help":
		err = r.handleHelp()
	case "exit", "quit":
		err = r.handleExit()
		quit = true
	default:
		err = writeOutput(r.out, "Unknown command: %s\n", cmd)
	}

	if err != nil {
		if writeErr := writeOutput(r.out, "Execution `%s` failed: %v\n", cmd, err); writeErr != nil {
			return commandResult{
				output: fmt.Sprintf("Execution `%s` failed: %v (writing output failed: %v)\n", cmd, err, writeErr),
				quit:   quit,
			}
		}
	}

	return commandResult{
		output: buf.String(),
		quit:   quit,
	}
}

func (r *replCommander) getMemcachedClient() (memcached.Client, error) {
	return r.cm.getCurrentClient()
}

/**
 * context operations
 */

func (r *replCommander) handleUse(_ context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: use <context>")
	}
	return r.cm.useContext(args[1])
}

func (r *replCommander) handleList(_ context.Context) error {
	ctx, _ := r.cm.getCurrentContext()
	for _, name := range r.cm.listContexts() {
		if ctx != nil && ctx.Name == name {
			if err := writeOutput(r.out, "* %s\n", name); err != nil {
				return err
			}
		} else {
			if err := writeOutput(r.out, "  %s\n", name); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *replCommander) handleCurrent(_ context.Context) error {
	ctx, err := r.cm.getCurrentContext()
	if err != nil {
		return err
	}
	return writeOutput(r.out, "Current context: %s\nServers: %s\n", ctx.Name, ctx.Servers)
}

/**
 * key-value operations
 */

func (r *replCommander) handleGet(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: get <key>")
	}

	client, err := r.getMemcachedClient()
	if err != nil {
		return err
	}
	item, err := client.MetaGet(
		ctx,
		[]byte(args[1]),
		memcached.MetaGetFlagReturnTTL(),
		memcached.MetaGetFlagReturnSize(),
		memcached.MetaGetFlagReturnValue(),
		memcached.MetaGetFlagReturnCAS(),
		memcached.MetaGetFlagReturnKey(),
		memcached.MetaGetFlagReturnClientFlags(),
		memcached.MetaGetFlagReturnLastAccessedTime(),
		memcached.MetaGetFlagReturnHitBefore(),
	)
	if err != nil {
		return ignoreMemcachedError(r.out, err)
	}
	return writeMetaItem(r.out, item)
}

func (r *replCommander) handleSet(ctx context.Context, args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: set <key> <value> [expiration]")
	}

	var expiration time.Duration
	if len(args) >= 4 {
		if e, err := strconv.ParseUint(args[3], 10, 32); err == nil {
			expiration = time.Duration(e) * time.Second
		}
	}

	client, err := r.getMemcachedClient()
	if err != nil {
		return err
	}
	if err := client.Set(ctx, args[1], []byte(args[2]), magicFlags, expiration); err != nil {
		return ignoreMemcachedError(r.out, err)
	}
	_, err = fmt.Fprintln(r.out, "OK")
	return err
}

func (r *replCommander) handleDelete(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: delete <key>")
	}

	client, err := r.getMemcachedClient()
	if err != nil {
		return err
	}
	if err := client.Delete(ctx, args[1]); err != nil {
		return ignoreMemcachedError(r.out, err)
	}
	_, err = fmt.Fprintln(r.out, "OK")
	return err
}

func (r *replCommander) handleIncr(ctx context.Context, args []string) error {
	if len(args) != 2 && len(args) != 3 {
		return fmt.Errorf("usage: incr <key> [delta]")
	}

	delta := uint64(1)
	if len(args) == 3 {
		if d, err := strconv.ParseUint(args[2], 10, 64); err == nil {
			delta = d
		}
	}
	client, err := r.getMemcachedClient()
	if err != nil {
		return err
	}
	newValue, err := client.Incr(ctx, args[1], delta)
	if err != nil {
		return ignoreMemcachedError(r.out, err)
	}
	return writeOutput(r.out, "%d\n", newValue)
}

func (r *replCommander) handleDecr(ctx context.Context, args []string) error {
	if len(args) != 2 && len(args) != 3 {
		return fmt.Errorf("usage: decr <key> [delta]")
	}

	delta := uint64(1)
	if len(args) == 3 {
		if d, err := strconv.ParseUint(args[2], 10, 64); err == nil {
			delta = d
		}
	}
	client, err := r.getMemcachedClient()
	if err != nil {
		return err
	}
	newValue, err := client.Decr(ctx, args[1], delta)
	if err != nil {
		return ignoreMemcachedError(r.out, err)
	}
	return writeOutput(r.out, "%d\n", newValue)
}

func (r *replCommander) handleTouch(ctx context.Context, args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("usage: touch <key> <expiration>")
	}

	expiration, err := strconv.ParseUint(args[2], 10, 32)
	if err != nil {
		return fmt.Errorf("invalid expiration format: %v", err)
	}
	client, err := r.getMemcachedClient()
	if err != nil {
		return err
	}
	if err := client.Touch(ctx, args[1], time.Duration(expiration)*time.Second); err != nil {
		return ignoreMemcachedError(r.out, err)
	}
	return writeOutput(r.out, "OK\n")
}

func (r *replCommander) handleMGet(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: gets <key1> [key2 ...]")
	}

	keys := make([]string, len(args)-1)
	copy(keys, args[1:])

	items := make([]*memcached.MetaItem, 0, len(keys))
	client, err := r.getMemcachedClient()
	if err != nil {
		return err
	}
	for _, key := range keys {
		item, err := client.MetaGet(
			ctx,
			[]byte(key),
			memcached.MetaGetFlagReturnTTL(),
			memcached.MetaGetFlagReturnSize(),
			memcached.MetaGetFlagReturnValue(),
			memcached.MetaGetFlagReturnCAS(),
			memcached.MetaGetFlagReturnKey(),
			memcached.MetaGetFlagReturnClientFlags(),
			memcached.MetaGetFlagReturnLastAccessedTime(),
			memcached.MetaGetFlagReturnHitBefore(),
		)
		if err != nil {
			if err := writeOutput(r.out, "Encounter error while getting key '%s': %v\n", key, rootErr(err)); err != nil {
				return err
			}
			continue
		}

		items = append(items, item)
	}

	return writeMetaItems(r.out, items)
}

/**
 * other operations
 */

func (r *replCommander) handleVersion(_ context.Context) error {
	return writeOutput(r.out, "Version: %s\n", version)
}

func (r *replCommander) handleHelp() error {
	return writeOutput(r.out, `Available commands:
  use <context>     Switch to a different context
  list              List all contexts
  current           Show current context
  get <key>         Get value by key
  gets <key...>     Get multiple values by keys
  set <key> <value> Set key to value
  delete <key>      Delete key
  incr <key> [delta] Increment value
  decr <key> [delta] Decrement value
  touch <key> <exp> Update expiration time
  help              Show this help message
  exit, quit        Exit the program
`)
}

func (r *replCommander) handleExit() error {
	return writeOutput(r.out, "Bye!\n")
}

func writeOutput(w io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(w, format, args...)
	return err
}
