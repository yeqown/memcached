package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/spf13/cobra"
)

/**
 * Context group commands
 */

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
	_ = cmd.MarkFlagRequired("servers")

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

/**
 * KV group commands
 */

const (
	magicFlags = 0x0705
	magicSeed  = 0x2014
)

func newKVGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "get [key]",
		Short: "Get value by key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager := getContextManager(cmd, false)
			client, err := manager.getCurrentClient()
			if err != nil {
				return err
			}
			value, err := client.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			fmt.Printf("%s\n", value)
			return nil
		},
	}
}

func newKVSetCommand() *cobra.Command {
	var expiration uint32

	cmd := &cobra.Command{
		Use:   "set [key] [value]",
		Short: "Set key to value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager := getContextManager(cmd, false)
			client, err := manager.getCurrentClient()
			if err != nil {
				return err
			}

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

func newKVDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete [key]",
		Short: "Delete key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager := getContextManager(cmd, false)
			client, err := manager.getCurrentClient()
			if err != nil {
				return err
			}

			err = client.Delete(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			fmt.Printf("OK\n")
			return nil
		},
	}
}

func newKVGetsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "gets [key]",
		Short: "Get value and CAS token by key",
		Long:  "Gets command retrieves the value and CAS token for the given key",
		Args:  cobra.MatchAll(),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager := getContextManager(cmd, false)
			client, err := manager.getCurrentClient()
			if err != nil {
				return err
			}

			fmt.Printf("keys: %v\n", args[1:])

			items, err := client.Gets(cmd.Context(), args[1:]...)
			if err != nil {
				return err
			}

			for idx, item := range items {
				fmt.Printf("[%d] %+v\n", idx, item)
			}

			return nil
		},
	}
}

func newKVTouchCommand() *cobra.Command {
	var expiration uint32

	cmd := &cobra.Command{
		Use:   "touch [key]",
		Short: "Update expiration time for a key",
		Long:  "Touch command updates the expiration time for an existing key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager := getContextManager(cmd, false)
			client, err := manager.getCurrentClient()
			if err != nil {
				return err
			}

			if err := client.Touch(cmd.Context(), args[0], expiration); err != nil {
				return err
			}

			fmt.Println("OK")
			return nil
		},
	}

	cmd.Flags().Uint32VarP(&expiration, "ttl", "t", 0, "new expiration time in seconds")
	_ = cmd.MarkFlagRequired("ttl")
	return cmd
}

func newKVFlushAllCommand() *cobra.Command {
	var delay uint32

	cmd := &cobra.Command{
		Use:   "flushall",
		Short: "Flush all keys from the cache",
		Long:  "Flushall command invalidates all existing items in memcached",
		RunE: func(cmd *cobra.Command, args []string) error {
			manager := getContextManager(cmd, false)
			client, err := manager.getCurrentClient()
			if err != nil {
				return err
			}

			left := delay
			var ticker *time.Ticker
			if delay <= 0 {
				goto imme
			}

			ticker = time.NewTicker(time.Second)
			for left > 0 {
				select {
				case <-ticker.C:
					fmt.Printf("The flush command would be send in %d seconds...\n", delay)
					left--
				case <-cmd.Context().Done():
					return cmd.Context().Err()
				}
			}

		imme:
			if err := client.FlushAll(cmd.Context()); err != nil {
				return err
			}

			fmt.Println("OK")
			return nil
		},
	}

	cmd.Flags().Uint32VarP(&delay, "delay", "d", 5, "delay in seconds before invalidating all items")
	return cmd
}
