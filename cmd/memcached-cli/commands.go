package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/spf13/cobra"
)

func newContextCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ctx",
		Short: "Manage memcached contexts",
		Long:  `Create, delete, switch between different memcached contexts.`,
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

func newContextCreateCommand() *cobra.Command {
	var (
		servers      string
		poolSize     int
		connTimeout  time.Duration
		readTimeout  time.Duration
		writeTimeout time.Duration
		hashStrategy string
	)

	cmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create a new context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := newContextManager()
			if err != nil {
				return err
			}

			if !lo.Contains([]string{"rendezvous", "crc32", "murmur3"}, hashStrategy) {
				return fmt.Errorf("hash strategy %s not supported", hashStrategy)
			}

			config := clientConfig{
				PoolSize:     poolSize,
				DialTimeout:  connTimeout,
				ReadTimeout:  readTimeout,
				WriteTimeout: writeTimeout,
				HashStrategy: hashStrategy,
			}

			if err = manager.newContext(args[0], servers, &config); err != nil {
				return err
			}

			fmt.Printf("Context %s created.\n", args[0])
			return nil
		},
	}

	cmd.Flags().StringVarP(&servers, "servers", "s", "", "comma-separated list of memcached servers")
	cmd.Flags().IntVarP(&poolSize, "pool-size", "p", 10, "connection pool size")
	cmd.Flags().DurationVar(&connTimeout, "connect-timeout", 5*time.Second, "dial timeout")
	cmd.Flags().DurationVar(&readTimeout, "read-timeout", 3*time.Second, "read timeout")
	cmd.Flags().DurationVar(&writeTimeout, "write-timeout", 3*time.Second, "write timeout")
	cmd.Flags().StringVar(&hashStrategy, "hash-strategy", "crc32", "hash strategy, one of: crc32(default), rendezvous, murmur3")
	cmd.MarkFlagRequired("servers")

	return cmd
}

func newContextListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all contexts",
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := newContextManager()
			if err != nil {
				return err
			}

			current, _ := manager.getCurrentContext()
			allContexts := manager.listContexts()
			if len(allContexts) == 0 {
				fmt.Println("No contexts found.")
			} else {
				fmt.Printf("Found %d Contexts:\n", len(allContexts))
				fmt.Println()
			}

			for _, name := range allContexts {
				if current != nil && current.Name == name {
					fmt.Printf("* %s\n", name)
				} else {
					fmt.Printf("  %s\n", name)
				}
			}

			return nil
		},
	}
}

func newContextUseCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "use [name]",
		Short: "Switch to a different context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := newContextManager()
			if err != nil {
				return err
			}

			if err = manager.useContext(args[0]); err != nil {
				return err
			}

			fmt.Printf("Switched to context %s.\n", args[0])
			return nil
		},
	}
}

func newContextDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete [name]",
		Short: "Delete a context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := newContextManager()
			if err != nil {
				return err
			}
			return manager.deleteContext(args[0])
		},
	}
}

func newContextCurrentCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show current context",
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := newContextManager()
			if err != nil {
				return err
			}

			ctx, err := manager.getCurrentContext()
			if err != nil {
				return err
			}

			// 处理服务器地址的换行显示
			servers := strings.Split(ctx.Servers, ",")

			fmt.Printf("📌 Current Context: %s\n", ctx.Name)
			fmt.Println("\nConfigurations:")
			fmt.Println("┌─────────────────────┬──────────────────────────────────────────────────────┐")
			fmt.Printf("│ %-20s│ %-52s │\n", "Param", "Value")
			fmt.Println("├─────────────────────┼──────────────────────────────────────────────────────┤")
			fmt.Printf("│ %-20s│ %-52s │\n", "Servers", fmt.Sprintf("%d instances:", len(servers)))
			for _, server := range servers {
				fmt.Printf("│                     │ %-52s │\n", server)
			}
			fmt.Printf("│ %-20s│ %-52d │\n", "ConnectionPoolSize", ctx.Config.PoolSize)
			fmt.Printf("│ %-20s│ %-52s │\n", "DialTimeout", ctx.Config.DialTimeout)
			fmt.Printf("│ %-20s│ %-52s │\n", "ReadTimeout", ctx.Config.ReadTimeout)
			fmt.Printf("│ %-20s│ %-52s │\n", "WriteTimeout", ctx.Config.WriteTimeout)
			fmt.Printf("│ %-20s│ %-52s │\n", "HashStrategy", ctx.Config.HashStrategy)
			fmt.Println("└─────────────────────┴──────────────────────────────────────────────────────┘")

			return nil
		},
	}
}

func newGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get [key]",
		Short: "Get value by key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClientFromCurrentContext()
			if err != nil {
				return err
			}
			defer client.Close()

			value, err := client.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			fmt.Printf("%s\n", value)
			return nil
		},
	}

	return cmd
}

func newSetCommand() *cobra.Command {
	var expiration uint32

	cmd := &cobra.Command{
		Use:   "set [key] [value]",
		Short: "Set key to value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClientFromCurrentContext()
			if err != nil {
				return err
			}
			defer client.Close()

			err = client.Set(cmd.Context(), args[0], []byte(args[1]), magicFlags, expiration)
			if err != nil {
				return err
			}

			fmt.Printf("OK\n")
			return nil
		},
	}

	cmd.Flags().Uint32VarP(&expiration, "ttl", "t", 0, "ttl of key in seconds")

	return cmd
}

func newDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete [key]",
		Short: "Delete key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClientFromCurrentContext()
			if err != nil {
				return err
			}
			defer client.Close()

			err = client.Delete(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			fmt.Printf("OK\n")
			return nil
		},
	}
}

const (
	magicFlags = 0x0705
	magicSeed  = 0x2014
)
