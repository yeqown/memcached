package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/MakeNowJust/heredoc"
	prompt "github.com/c-bata/go-prompt"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/yeqown/log"
)

const (
	version = "v1.0.0"
)

var (
	logger = newLogger()
)

func main() {
	var (
		timeout time.Duration
		verbose bool
	)

	rootCmd := &cobra.Command{
		Use:   "memcached-cli",
		Short: "A command line interface for memcached",
		Long:  `A command line interface for memcached with context management and interactive mode.`,
		PreRun: func(cmd *cobra.Command, args []string) {
			if verbose {
				logger.SetLogLevel(log.LevelDebug)
				logger.SetCallerReporter(true)
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAsREPL(timeout)
		},
	}

	// 添加全局标志
	rootCmd.PersistentFlags().DurationVarP(
		&timeout, "timeout", "", 10*time.Second, "timeout for interactive mode, default 10s")
	rootCmd.PersistentFlags().BoolVarP(
		&verbose, "verbose", "v", false, "enable verbose mode")

	// add version command
	rootCmd.AddCommand(newVersionCommand())

	rootCmd.AddCommand(
		newContextCommand(), // add context manage sub commands
		newKVCommand(),      // add kv sub commands
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func runAsREPL(timeout time.Duration) error {
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

	// If current context is not set, print instructions to set one.

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
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			manager, err := newContextManager()
			if err != nil {
				logger.Warnf("failed to create context manager: %v", err)
			}
			storeContextManager(cmd, manager)
			return nil
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
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			manager, err := newContextManager()
			if err != nil {
				logger.Warnf("failed to create context manager: %v", err)
			}
			storeContextManager(cmd, manager)
			storeTemporaryContextName(cmd, contextName)
			return nil
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			manager := getContextManager(cmd, false)
			return manager.close()
		},
	}

	cmd.PersistentFlags().StringVarP(
		&contextName,
		"context", "c", "", "context name to use, if not set, use current context")

	cmd.AddCommand(
		newKVSetCommand(),
		newKVGetCommand(),
		newKVGetsCommand(),
		newKVDeleteCommand(),
		newKVTouchCommand(),
		newKVFlushAllCommand(),
	)

	return cmd
}

type contextManagerKeyType struct{}

var contextManagerKey = contextManagerKeyType{}

type contextNameKeyType struct{}

var contextNameKey = contextNameKeyType{}

func storeContextManager(cmd *cobra.Command, manager *contextManager) {
	newCtx := context.WithValue(cmd.Context(), contextManagerKey, manager)
	cmd.SetContext(newCtx)
}

func getContextManager(cmd *cobra.Command, recreate bool) *contextManager {
	cm, ok := cmd.Context().Value(contextManagerKey).(*contextManager)
	if ok {
		return cm
	}

	if !recreate {
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
