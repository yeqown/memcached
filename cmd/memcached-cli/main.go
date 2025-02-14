package main

import (
	"fmt"
	"os"
	"time"

	"github.com/MakeNowJust/heredoc"
	"github.com/c-bata/go-prompt"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

const (
	version = "v0.1.0"
)

func main() {
	var timeout time.Duration

	rootCmd := &cobra.Command{
		Use:   "memcached-cli",
		Short: "A command line interface for memcached",
		Long:  `A command line interface for memcached with context management and interactive mode.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAsREPL(timeout)
		},
	}

	// 添加全局标志
	rootCmd.PersistentFlags().DurationVarP(
		&timeout,
		"timeout", "", 10*time.Second, "timeout for interactive mode, default 10s")

	// 添加版本命令
	rootCmd.AddCommand(newVersionCommand())

	// 添加上下文管理命令
	rootCmd.AddCommand(
		newContextCommand(),
	)

	// 添加数据操作命令
	rootCmd.AddCommand(
		newGetCommand(),
		newSetCommand(),
		newDeleteCommand(),
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
