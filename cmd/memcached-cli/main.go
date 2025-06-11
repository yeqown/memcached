package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/MakeNowJust/heredoc"
	prompt "github.com/c-bata/go-prompt"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/yeqown/log"
)

const (
	version = "v1.3.0"
)

var (
	logger = newLogger()
)

func main() {
	var (
		// temporary context variables, allows using without an existed context.
		servers      string
		hashStrategy string

		timeout time.Duration
		verbose bool
	)

	rootCmd := &cobra.Command{
		Use:   "memcached-cli",
		Short: "A command line interface for memcached",
		Long:  `A command line interface for memcached with context management and interactive mode.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if verbose {
				logger.SetLogLevel(log.LevelDebug)
				logger.SetCallerReporter(true)
			}

			logger.Debugf("rootCmd.PreRun: timeout=%v, verbose=%v", timeout, verbose)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAsREPL(timeout, servers, hashStrategy)
		},
	}

	// 添加全局标志
	rootCmd.PersistentFlags().StringVarP(
		&servers, "servers", "s", "", "memcached server addresses, separated by comma, e.g. '127.0.0.1:11211'")
	rootCmd.PersistentFlags().StringVarP(
		&hashStrategy, "hash", "d", "rendezvous", "hash distribution algorithm: crc32, murmur3, rendezvous(default)")

	rootCmd.PersistentFlags().DurationVarP(
		&timeout, "timeout", "", 10*time.Second, "timeout for interactive mode, default 10s")
	rootCmd.PersistentFlags().BoolVarP(
		&verbose, "verbose", "v", false, "enable verbose mode")

	rootCmd.AddCommand(
		newVersionCommand(), // add version command
		newContextCommand(), // add context manage sub commands
		newKVCommand(),      // add kv sub commands
		newHistoryCommand(), // add history sub commands
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func runAsREPL(timeout time.Duration, servers, hashStrategy string) error {
	fmt.Println(heredoc.Doc(`
		Welcome to memcached-cli
		Type 'help' to see available commands
		Type 'exit' or 'quit' to exit.

		If you have any issue, please report it to:
		https://github.com/yeqown/memcached/issues/new.
	`))

	manager, err := newContextManager()
	if err != nil {
		return errors.Wrap(err, "failed to create context manager")
	}

	// if servers are not empty, create a temporary context
	if servers = strings.TrimSpace(servers); servers != "" {
		logger.Debugf("adding servers: %v to temporary context as 'temporary'", servers)
		manager.addTemporaryContext(servers, hashStrategy)
	}

	contexts := manager.listContexts()
	// If no context available, print instructions to create one.
	if len(contexts) == 0 {
		fmt.Println(heredoc.Doc(`
			Hint: There's no available context, please create one by using command:
			memcached-cli ctx create example-ctx -s 'localhost:11211'
		`))
	}

	current, err := manager.getCurrentContext()
	if err != nil {
		return errors.Wrap(err, "failed to get current context")
	}

	if len(contexts) > 0 && current == nil {
		fmt.Println(heredoc.Doc(`
			Hint: current context is empty, you can set one by using command:
			bash$ memcached-cli ctx use example-ctx

			Or you can switch in interactive mode by typing:
			>>> use example-ctx

			To list all available contexts, type:
			>>> list
		`))
	}

	// If the current context is not set, print instructions to set one.
	repl, err := newREPLCommander(manager, timeout)
	if err != nil {
		return err
	}

	p := prompt.New(
		repl.commandExecutor,
		repl.commandCompleter,
		prompt.OptionTitle("memcached-cli"),
		prompt.OptionPrefix(">>> "),
		prompt.OptionInputTextColor(prompt.Yellow),
	)

	p.Run()
	return nil
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("memcached-cli version %s\n", version)
		},
	}
}

func newContextCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ctx",
		Short: "Manage memcached contexts",
		Long:  `Create, delete, switch between different memcached contexts.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			cmd.Root().PersistentPreRun(cmd, args)
			manager, err := newContextManager()
			if err != nil {
				logger.Warnf("failed to create context manager: %v", err)
			}
			storeContextManager(cmd, manager)
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			manager := getContextManager(cmd, false)
			return manager.close()
		},
	}

	cmd.AddCommand(
		newContextCreateCommand(),
		newContextListCommand(),
		newContextUseCommand(),
		newContextDeleteCommand(),
		newContextCurrentCommand(),
	)

	return cmd
}

func newKVCommand() *cobra.Command {

	var contextName string

	cmd := &cobra.Command{
		Use:          "kv",
		Short:        "Manage key-value operations",
		Long:         `Perform key-value operations like get, set, and delete.`,
		SilenceUsage: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			cmd.Root().PersistentPreRun(cmd, args)

			manager, err := newContextManager()
			if err != nil {
				logger.Warnf("failed to create context manager: %v", err)
			}
			storeContextManager(cmd, manager)
			storeTemporaryContextName(cmd, contextName)
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			manager := getContextManager(cmd, false)
			// save history
			history := manager.getHistoryManager()
			if history != nil {
				if err := history.close(); err != nil {
					logger.Warnf("failed to save history: %v", err)
				}
			}

			// save context
			if err := manager.close(); err != nil {
				logger.Warnf("failed to save context: %v", err)
			}

			return nil
		},
	}

	cmd.PersistentFlags().StringVarP(
		&contextName,
		"context", "c", "", "context name to use, if not set, use current context")

	cmd.AddCommand(
		newKVGetCommand(),
		newKVSetCommand(),
		newKVDeleteCommand(),
		newKVGetsCommand(),
		newKVTouchCommand(),
		newKVFlushAllCommand(),
	)

	return cmd
}

func newHistoryCommand() *cobra.Command {
	var (
		keyword string
		since   string
		until   string
		limit   int
	)

	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show command history",
		Long:  "Display or search kv command history",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			cmd.Root().PersistentPreRun(cmd, args)

			manager, err := newContextManager()
			if err != nil {
				logger.Warnf("failed to create context manager: %v", err)
			}
			storeContextManager(cmd, manager)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			history := getContextManager(cmd, false).getHistoryManager()
			results := history.search(keyword, since, until, limit)

			if len(results) == 0 {
				fmt.Println("No history found.")
				return nil
			}

			fmt.Printf("KV Command History(limit=%d): \n", limit)
			for _, h := range results {
				args := h.Args
				if len(args) > 50 {
					args = args[:47] + "..."
				}
				fmt.Printf("%s  %s  %s\n",
					time.Unix(h.Timestamp, 0).Format(historyTimeFormat),
					h.Command,
					args,
				)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&keyword, "keyword", "k", "", "search keyword")
	cmd.Flags().StringVar(&since, "since", "", fmt.Sprintf("show history since time (format: %s)", historyTimeFormat))
	cmd.Flags().StringVar(&until, "until", "", fmt.Sprintf("show history until time (format: %s)", historyTimeFormat))
	cmd.Flags().IntVarP(&limit, "limit", "n", 100, "limit output to n records")

	cmd.AddCommand(
		newHistoryEnableCommand(),
		newHistoryDisableCommand(),
	)

	return cmd
}

type contextManagerKeyType struct{}

var contextManagerKey = contextManagerKeyType{}

type contextNameKeyType struct{}

var contextNameKey = contextNameKeyType{}

func storeContextManager(cmd *cobra.Command, manager *contextManager) {
	logger.Debugf("store context manager: %+v", manager)
	newCtx := context.WithValue(cmd.Context(), contextManagerKey, manager)
	cmd.SetContext(newCtx)
}

func getContextManager(cmd *cobra.Command, recreate bool) *contextManager {
	cm, ok := cmd.Context().Value(contextManagerKey).(*contextManager)
	if ok {
		return cm
	}

	if !recreate {
		logger.Warnf("context manager not found, recreate it")
		return nil
	}

	cm, err := newContextManager()
	if err != nil {
		panic(err)
	}
	storeContextManager(cmd, cm)

	return cm
}

func storeTemporaryContextName(cmd *cobra.Command, name string) {
	if len(name) == 0 || cmd == nil {
		return
	}

	newCtx := context.WithValue(cmd.Context(), contextNameKey, name)
	cmd.SetContext(newCtx)
}

func getTemporaryContextName(cmd *cobra.Command) string {
	name, ok := cmd.Context().Value(contextNameKey).(string)
	if ok {
		return name
	}

	return ""
}

func newLogger() *log.Logger {
	l, err := log.NewLogger(
		log.WithLevel(log.LevelInfo),
		log.WithTimeFormat(true, "2006-01-02 15:04:05"),
	)
	if err != nil {
		panic(err)
	}

	return l
}
