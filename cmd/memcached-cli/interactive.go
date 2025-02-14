package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/c-bata/go-prompt"
	"github.com/yeqown/memcached"
)

type replCommander struct {
	cm     *contextManager
	client memcached.Client
}

func newInteractiveMode() (*replCommander, error) {
	manager, err := newContextManager()
	if err != nil {
		return nil, err
	}

	return &replCommander{
		cm: manager,
	}, nil
}

func (r *replCommander) ensureClient() error {
	if r.client != nil {
		return nil
	}

	ctx, err := r.cm.getCurrentContext()
	if err != nil {
		return err
	}

	client, err := createClient(ctx)
	if err != nil {
		return err
	}

	r.client = client
	return nil
}

func (r *replCommander) completer(d prompt.Document) []prompt.Suggest {
	suggestions := []prompt.Suggest{
		// context operations
		{Text: "use", Description: "Switch to a different context"},
		{Text: "list", Description: "List all contexts"},
		{Text: "current", Description: "Show current context"},
		// key-value operations
		{Text: "get", Description: "Get value by key"},
		{Text: "mget", Description: "Get multiple values by keys"},
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

	return prompt.FilterHasPrefix(suggestions, d.GetWordBeforeCursor(), true)
}

func (r *replCommander) executor(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	args := strings.Fields(line)
	cmd := args[0]

	var (
		err         error
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	)
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
	case "mget":
		err = r.handleMGet(ctx, args)

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
		fmt.Printf("Error: %v\n", err)
	}
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
	if err := r.ensureClient(); err != nil {
		return err
	}
	value, err := r.client.Get(ctx, args[1])
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", value)
	return nil
}

func (r *replCommander) handleSet(ctx context.Context, args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("usage: set <key> <value> [expiration]")
	}
	if err := r.ensureClient(); err != nil {
		return err
	}

	expiration := uint32(0)
	if len(args) == 4 {
		if e, err := strconv.ParseUint(args[2], 10, 32); err == nil {
			expiration = uint32(e)
		}
	}

	if err := r.client.Set(ctx, args[1], []byte(args[2]), magicFlags, expiration); err != nil {
		return err
	}
	fmt.Println("OK")
	return nil
}

func (r *replCommander) handleDelete(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: delete <key>")
	}
	if err := r.ensureClient(); err != nil {
		return err
	}
	if err := r.client.Delete(ctx, args[1]); err != nil {
		return err
	}
	fmt.Println("OK")
	return nil
}

func (r *replCommander) handleIncr(ctx context.Context, args []string) error {
	if len(args) != 2 && len(args) != 3 {
		return fmt.Errorf("usage: incr <key> [delta]")
	}
	if err := r.ensureClient(); err != nil {
		return err
	}
	delta := uint64(1)
	if len(args) == 3 {
		if d, err := strconv.ParseUint(args[2], 10, 64); err == nil {
			delta = d
		}
	}
	newValue, err := r.client.Incr(ctx, args[1], delta)
	if err != nil {
		return err
	}
	fmt.Printf("%d\n", newValue)
	return nil
}

func (r *replCommander) handleDecr(ctx context.Context, args []string) error {
	if len(args) != 2 && len(args) != 3 {
		return fmt.Errorf("usage: decr <key> [delta]")
	}
	if err := r.ensureClient(); err != nil {
		return err
	}
	delta := uint64(1)
	if len(args) == 3 {
		if d, err := strconv.ParseUint(args[2], 10, 64); err == nil {
			delta = d
		}
	}
	newValue, err := r.client.Decr(ctx, args[1], delta)
	if err != nil {
		return err
	}
	fmt.Printf("%d\n", newValue)
	return nil
}

func (r *replCommander) handleTouch(ctx context.Context, args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("usage: touch <key> <expiration>")
	}
	if err := r.ensureClient(); err != nil {
		return err
	}
	expiration, err := strconv.ParseUint(args[2], 10, 32)
	if err != nil {
		return fmt.Errorf("invalid expiration format: %v", err)
	}
	if err := r.client.Touch(ctx, args[1], uint32(expiration)); err != nil {
		return err
	}
	fmt.Println("OK")
	return nil
}

func (r *replCommander) handleMGet(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: mget <key1> [key2 ...]")
	}
	if err := r.ensureClient(); err != nil {
		return err
	}
	keys := make([]string, len(args)-1)
	for i, key := range args[1:] {
		keys[i] = key
	}
	values, err := r.client.Gets(ctx, keys...)
	if err != nil {
		return err
	}
	for idx, item := range values {
		fmt.Printf("[%d]:%+v\n", idx, item)
	}
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
	fmt.Println("  mget <key...>     Get multiple values by keys")
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
