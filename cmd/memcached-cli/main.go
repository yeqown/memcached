package main

import (
	"fmt"
	"os"

	"github.com/c-bata/go-prompt"
	"github.com/spf13/cobra"
)

const (
	version = "v0.1.0"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "memcached-cli",
		Short: "A command line interface for memcached",
		Long:  `A command line interface for memcached with context management and interactive mode.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return startInteractiveMode()
		},
	}

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

func startInteractiveMode() error {
	im, err := newInteractiveMode()
	if err != nil {
		return err
	}

	fmt.Println("Welcome to memcached-cli interactive mode")
	fmt.Println("Type 'exit' or 'quit' to exit")

	p := prompt.New(
		im.executor,
		im.completer,
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
