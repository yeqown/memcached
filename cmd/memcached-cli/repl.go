package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/c-bata/go-prompt"
	"github.com/pkg/errors"
	"github.com/yeqown/memcached"
)

type replCommander struct {
	cm      *contextManager
	timeout time.Duration
}

func newREPLCommander(manager *contextManager, timeout time.Duration) (*replCommander, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	return &replCommander{
		cm:      manager,
		timeout: timeout,
	}, nil
}

func (r *replCommander) commandCompleter(d prompt.Document) []prompt.Suggest {
	suggestions := []prompt.Suggest{
		// context operations
		{Text: "use", Description: "Switch to a different context"},
		{Text: "list", Description: "List all contexts"},
		{Text: "current", Description: "Show current context"},
		// key-value operations
		{Text: "get", Description: "Get value by key"},
		{Text: "gets", Description: "Get multiple values by keys"},
		{Text: "set", Description: "Set key to value"},
		{Text: "delete", Description: "Delete key"},
		{Text: "incr", Description: "Increment value"},
		{Text: "decr", Description: "Decrement value"},
		{Text: "touch", Description: "Update expiration time"},
		// other
		{Text: "version", Description: "Show version information"},
		{Text: "help", Description: "Show help message"},
		{Text: "exit", Description: "Exit the program"},
		{Text: "quit", Description: "Exit the program"},
	}

	sub := d.GetWordBeforeCursor()

	if sub == "" {
		return []prompt.Suggest{}
	}

	return prompt.FilterHasPrefix(suggestions, sub, true)
}

func (r *replCommander) commandExecutor(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	args := strings.Fields(line)
	cmd := args[0]

	var err error
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()

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
		r.handleHelp()
	case "exit", "quit":
		r.handleExit()
	default:
		fmt.Printf("Unknown command: %s\n", cmd)
	}

	if err != nil {
		fmt.Printf("Execution `%s` failed: %v\n", cmd, err)
	}
}

func (r *replCommander) getMemcachedClient() memcached.Client {
	client, err := r.cm.getCurrentClient()
	if err != nil {
		panic(err)
	}

	return client
}

/**
 * context operations
 */

func (r *replCommander) handleUse(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: use <context>")
	}
	return r.cm.useContext(args[1])
}

func (r *replCommander) handleList(_ context.Context) error {
	ctx, _ := r.cm.getCurrentContext()
	for _, name := range r.cm.listContexts() {
		if ctx != nil && ctx.Name == name {
			fmt.Printf("* %s\n", name)
		} else {
			fmt.Printf("  %s\n", name)
		}
	}
	return nil
}

func (r *replCommander) handleCurrent(_ context.Context) error {
	ctx, err := r.cm.getCurrentContext()
	if err != nil {
		return err
	}
	fmt.Printf("Current context: %s\n", ctx.Name)
	fmt.Printf("Servers: %s\n", ctx.Servers)
	return nil
}

/**
 * key-value operations
 */

func (r *replCommander) handleGet(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: get <key>")
	}

	item, err := r.getMemcachedClient().MetaGet(
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
		return ignoreMemcachedError(err)
	}
	printMetaItem(item)
	return nil
}

func (r *replCommander) handleSet(ctx context.Context, args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("usage: set <key> <value> [expiration]")
	}

	var expiration time.Duration
	if len(args) == 4 {
		if e, err := strconv.ParseUint(args[2], 10, 32); err == nil {
			expiration = time.Duration(e) * time.Second
		}
	}

	if err := r.getMemcachedClient().Set(ctx, args[1], []byte(args[2]), magicFlags, expiration); err != nil {
		return ignoreMemcachedError(err)
	}
	fmt.Println("OK")
	return nil
}

func (r *replCommander) handleDelete(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: delete <key>")
	}

	if err := r.getMemcachedClient().Delete(ctx, args[1]); err != nil {
		return ignoreMemcachedError(err)
	}
	fmt.Println("OK")
	return nil
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
	newValue, err := r.getMemcachedClient().Incr(ctx, args[1], delta)
	if err != nil {
		return ignoreMemcachedError(err)
	}
	fmt.Printf("%d\n", newValue)
	return nil
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
	newValue, err := r.getMemcachedClient().Decr(ctx, args[1], delta)
	if err != nil {
		return ignoreMemcachedError(err)
	}
	fmt.Printf("%d\n", newValue)
	return nil
}

func (r *replCommander) handleTouch(ctx context.Context, args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("usage: touch <key> <expiration>")
	}

	expiration, err := strconv.ParseUint(args[2], 10, 32)
	if err != nil {
		return fmt.Errorf("invalid expiration format: %v", err)
	}
	if err := r.getMemcachedClient().Touch(ctx, args[1], time.Duration(expiration)*time.Second); err != nil {
		return ignoreMemcachedError(err)
	}
	fmt.Println("OK")
	return nil
}

func (r *replCommander) handleMGet(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: mget <key1> [key2 ...]")
	}

	keys := make([]string, len(args)-1)
	copy(keys, args[1:])

	items := make([]*memcached.MetaItem, 0, len(keys))
	for _, key := range keys {
		item, err := r.getMemcachedClient().MetaGet(
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
			fmt.Printf("Encounter error while getting key '%s': %v\n", key, errors.Cause(err))
			continue
		}

		items = append(items, item)
	}

	printMetaItems(items)

	return nil
}

/**
 * other operations
 */

func (r *replCommander) handleVersion(_ context.Context) error {
	fmt.Printf("Version: %s\n", version)
	return nil
}

func (r *replCommander) handleHelp() {
	fmt.Println("Available commands:")
	fmt.Println("  use <context>     Switch to a different context")
	fmt.Println("  list              List all contexts")
	fmt.Println("  current           Show current context")

	fmt.Println("  get <key>         Get value by key")
	fmt.Println("  gets <key...>     Get multiple values by keys")
	fmt.Println("  set <key> <value> Set key to value")
	fmt.Println("  delete <key>      Delete key")
	fmt.Println("  incr <key> [delta] Increment value")
	fmt.Println("  decr <key> [delta] Decrement value")
	fmt.Println("  touch <key> <exp> Update expiration time")

	fmt.Println("  help              Show this help message")
	fmt.Println("  exit, quit        Exit the program")
}

func (r *replCommander) handleExit() {
	fmt.Println("Bye!")
	os.Exit(0)
}
