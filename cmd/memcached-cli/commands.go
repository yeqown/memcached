package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"github.com/yeqown/memcached"
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
			manager := getContextManager(cmd, false)
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

			if err := manager.newContext(args[0], servers, &config); err != nil {
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
			manager := getContextManager(cmd, false)
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
			manager := getContextManager(cmd, false)
			if err := manager.useContext(args[0]); err != nil {
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
			manager := getContextManager(cmd, false)
			return manager.deleteContext(args[0])
		},
	}
}

func newContextCurrentCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show current context",
		RunE: func(cmd *cobra.Command, args []string) error {
			manager := getContextManager(cmd, false)

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
	magicFlags uint32 = 0x0705
	magicSeed  uint64 = 0x2014

	historyTimeFormat string = "2006-01-02 15:04:05"
)

func newKVGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:          "get [key]",
		Short:        "Get value by key",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			manager := getContextManager(cmd, false)
			history := manager.getHistoryManager()
			client, err := manager.getClientWithContext(getTemporaryContextName(cmd))
			if err != nil {
				return err
			}
			item, err := client.MetaGet(
				cmd.Context(),
				[]byte(args[0]), // key
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
				return ignoreMemcachedError(os.Stdout, err)
			}

			recordHistory(cmd, history, "get", args)

			return printMetaItem(item)
		},
	}
}

// 修改 KV 命令，添加历史记录
func newKVSetCommand() *cobra.Command {
	var expiration time.Duration

	cmd := &cobra.Command{
		Use:          "set [key] [value]",
		Short:        "Set key to value",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			manager := getContextManager(cmd, false)
			history := manager.getHistoryManager()
			client, err := manager.getClientWithContext(getTemporaryContextName(cmd))
			if err != nil {
				return err
			}

			err = client.Set(cmd.Context(), args[0], []byte(args[1]), magicFlags, expiration)
			if err != nil {
				return ignoreMemcachedError(os.Stdout, err)
			}

			recordHistory(cmd, history, "set", args)

			fmt.Printf("OK\n")
			return nil
		},
	}

	cmd.Flags().DurationVarP(&expiration, "ttl", "t", 0, "ttl of key in seconds")
	return cmd
}

func newKVDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:          "delete [key]",
		Short:        "Delete key",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			manager := getContextManager(cmd, false)
			history := manager.getHistoryManager()
			client, err := manager.getClientWithContext(getTemporaryContextName(cmd))
			if err != nil {
				return err
			}

			err = client.Delete(cmd.Context(), args[0])
			if err != nil {
				return ignoreMemcachedError(os.Stdout, err)
			}

			recordHistory(cmd, history, "delete", args)

			fmt.Printf("OK\n")
			return nil
		},
	}
}

func newKVGetsCommand() *cobra.Command {
	return &cobra.Command{
		Use:          "gets [key]",
		Short:        "Get value and CAS token by key",
		Long:         "Gets command retrieves the value and CAS token for the given key",
		Args:         cobra.MatchAll(),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			manager := getContextManager(cmd, false)
			history := manager.getHistoryManager()
			client, err := manager.getClientWithContext(getTemporaryContextName(cmd))
			if err != nil {
				return err
			}

			items := make([]*memcached.MetaItem, 0, len(args))
			for _, key := range args {
				item, err := client.MetaGet(
					cmd.Context(),
					[]byte(key), // key
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
					fmt.Printf("Encounter an error while getting key '%s': %v\n", key, rootErr(err))
					continue
				}

				items = append(items, item)
			}

			recordHistory(cmd, history, "gets", args)

			return printMetaItems(items)
		},
	}
}

func newKVTouchCommand() *cobra.Command {
	var expiration time.Duration

	cmd := &cobra.Command{
		Use:          "touch [key] [flags]",
		Short:        "Update expiration time for a key",
		Long:         "Touch command updates the expiration time for an existing key",
		Example:      "memcached-cli kv touch foo -t 1m",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			manager := getContextManager(cmd, false)
			history := manager.getHistoryManager()
			client, err := manager.getClientWithContext(getTemporaryContextName(cmd))
			if err != nil {
				return err
			}

			if err := client.Touch(cmd.Context(), args[0], expiration); err != nil {
				return ignoreMemcachedError(os.Stdout, err)
			}

			recordHistory(cmd, history, "touch", args)

			fmt.Println("OK")
			return nil
		},
	}

	cmd.Flags().DurationVarP(&expiration, "ttl", "t", 0, "new expiration time in seconds")
	_ = cmd.MarkFlagRequired("ttl")
	return cmd
}

func newKVFlushAllCommand() *cobra.Command {
	var delay uint32

	cmd := &cobra.Command{
		Use:          "flushall",
		Short:        "Flush all keys from the cache",
		Long:         "Flushall command invalidates all existing items in memcached",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			manager := getContextManager(cmd, false)
			history := manager.getHistoryManager()
			client, err := manager.getClientWithContext(getTemporaryContextName(cmd))
			if err != nil {
				return err
			}

			left := int(delay)
			var ticker *time.Ticker
			if delay <= 0 {
				goto imme
			}

			ticker = time.NewTicker(time.Second)
			defer ticker.Stop()

			fmt.Print("Flush All delayed...\n") //
			for left >= 0 {
				select {
				case <-ticker.C:
					progress := int(float64(int(delay)-left) / float64(delay) * 20)
					if progress > 20 {
						progress = 20
					}
					fmt.Printf("\r%s%s %d seconds left to execute, ctrl+C to cancel anyway",
						strings.Repeat("█", progress),
						strings.Repeat("░", 20-progress),
						left)
					left--
				case <-cmd.Context().Done():
					fmt.Println("\nOperation cancelled")
					return cmd.Context().Err()
				}
			}
			fmt.Println()

		imme:
			if err := client.FlushAll(cmd.Context()); err != nil {
				return ignoreMemcachedError(os.Stdout, err)
			}

			recordHistory(cmd, history, "flushall", args)

			fmt.Println("OK")
			return nil
		},
	}

	cmd.Flags().Uint32VarP(&delay, "delay", "d", 5, "delay in seconds before invalidating all items")
	return cmd
}

func printMetaItems(items []*memcached.MetaItem) error {
	return writeMetaItems(os.Stdout, items)
}

func printMetaItem(item *memcached.MetaItem) error {
	return writeMetaItem(os.Stdout, item)
}

func writeMetaItems(w io.Writer, items []*memcached.MetaItem) error {
	for idx, item := range items {
		if _, err := io.WriteString(w, fmt.Sprintf(" ================= The [%d] item =================\n", idx)); err != nil {
			return err
		}
		if err := writeMetaItem(w, item); err != nil {
			return err
		}
	}

	return nil
}

func writeMetaItem(w io.Writer, item *memcached.MetaItem) error {
	lastAccessAt := time.Now().Add(-time.Duration(item.LastAccessedTime) * time.Second)
	output := fmt.Sprintf("Key:              %s\n", item.Key) +
		fmt.Sprintf("Flags:            %d (0x%x)\n", item.Flags, item.Flags) +
		fmt.Sprintf("CAS:              %d (0x%x)\n", item.CAS, item.CAS) +
		fmt.Sprintf("ClientFlags:      %d (0x%x)\n", item.Flags, item.Flags) +
		fmt.Sprintf("LastAccessedTime: %s (%s)\n", lastAccessAt.Format(time.RFC3339), formatSeconds(int(item.LastAccessedTime), "before", "never")) +
		fmt.Sprintf("HitBefore:        %s\n", map[bool]string{true: "✅", false: "❌"}[item.HitBefore]) +
		fmt.Sprintf("TTL:              %d (%s)\n", item.TTL, formatSeconds(int(item.TTL), "later", "never expires")) +
		fmt.Sprintf("Value:            %s\n\n", item.Value)
	_, err := io.WriteString(w, output)
	return err
}

func recordHistory(cmd *cobra.Command, history *kvCommandHistoryManager, command string, args []string) {
	if err := history.addRecord(command, args); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to record %s command in history: %v\n", command, err)
	}
}

func formatSeconds(seconds int, suffix, zeroString string) (readable string) {
	if seconds <= 0 {
		return zeroString
	}

	d := time.Duration(seconds) * time.Second
	if d.Hours() >= 1 {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		s := int(d.Seconds()) % 60
		readable = fmt.Sprintf("%dh", h)
		if m > 0 {
			readable += fmt.Sprintf("%dm", m)
		}
		if s > 0 {
			readable += fmt.Sprintf("%ds", s)
		}
	} else if d.Minutes() >= 1 {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		readable = fmt.Sprintf("%dm", m)
		if s > 0 {
			readable += fmt.Sprintf("%ds", s)
		}
	} else {
		readable = fmt.Sprintf("%ds", int(d.Seconds()))
	}

	return readable + " " + suffix
}

func ignoreMemcachedError(w io.Writer, err error) error {
	if err == nil {
		return nil
	}

	logger.Debugf("ignoreMemcachedError handling err: %v", err)

	memErrs := []error{
		memcached.ErrNonexistentCommand,
		memcached.ErrClientError,
		memcached.ErrServerError,
		memcached.ErrNotFound,
		memcached.ErrExists,
		memcached.ErrNotStored,
		memcached.ErrAuthenticationUnSupported,
		memcached.ErrAuthenticationFailed,
		memcached.ErrInvalidArgument,
		memcached.ErrNotSupported,
		memcached.ErrMalformedResponse,
		memcached.ErrUnknownIndicator,
		memcached.ErrInvalidAddress,
		memcached.ErrInvalidKey,
		memcached.ErrInvalidValue,
		memcached.ErrInvalidBinaryProtocol,
	}

	for _, memErr := range memErrs {
		if errors.Is(err, memErr) {
			_, writeErr := fmt.Fprintf(w, "Memcached Error: %v\n", rootErr(err))
			return writeErr
		}
	}

	return err
}

/**
 * History group commands
 */

func newHistoryEnableCommand() *cobra.Command {
	var historyMaxLines uint

	cmd := &cobra.Command{
		Use:          "enable",
		Short:        "Enable history recording",
		Long:         "Enable history recording",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			manager := getContextManager(cmd, false)
			manager.historyEnabled = true
			manager.historyMaxLines = int(historyMaxLines)
			if err := manager.save(); err != nil {
				return fmt.Errorf("save history settings: %w", err)
			}
			fmt.Println("History enabled!")
			return nil
		},
	}

	cmd.Flags().UintVarP(&historyMaxLines, "max-lines", "m", 10000, "max lines of history")

	return cmd
}

func newHistoryDisableCommand() *cobra.Command {
	return &cobra.Command{
		Use:          "disable",
		Short:        "Disable history",
		Long:         "Disable history",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			manager := getContextManager(cmd, false)
			manager.historyEnabled = false
			if err := manager.save(); err != nil {
				return fmt.Errorf("save history settings: %w", err)
			}
			fmt.Println("History disabled!")
			return nil
		},
	}
}
