package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type kvCommandHistory struct {
	Timestamp int64
	Command   string
	Args      string
}

type kvCommandHistoryManager struct {
	maxSize  int
	disabled bool
	file     *os.File // 保持文件句柄打开
}

const maxHistoryRecordSize = 2 * 1024 * 1024

func newHistoryManager(enabled bool, maxLines int) (*kvCommandHistoryManager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	historyDir := filepath.Join(home, ".memcached-cli")
	if err := os.MkdirAll(historyDir, 0o700); err != nil {
		return nil, err
	}

	root, err := os.OpenRoot(historyDir)
	if err != nil {
		return nil, err
	}

	// 以追加模式打开文件
	file, err := root.OpenFile(".history", os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	if err := root.Close(); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if err := file.Chmod(0o600); err != nil {
		return nil, errors.Join(err, file.Close())
	}

	hm := &kvCommandHistoryManager{
		maxSize:  maxLines,
		disabled: !enabled,
		file:     file,
	}

	return hm, nil
}

func (hm *kvCommandHistoryManager) close() error {
	if hm == nil || hm.file == nil {
		return nil
	}

	return hm.file.Close()
}

func (hm *kvCommandHistoryManager) addRecord(cmd string, args []string) error {
	if hm == nil || hm.disabled || hm.file == nil {
		fmt.Println("History is not available.")
		return nil
	}

	history := kvCommandHistory{
		Timestamp: time.Now().Unix(),
		Command:   cmd,
		Args:      strings.Join(args, " "),
	}

	line := fmt.Sprintf("%d %s %s\n", history.Timestamp, history.Command, history.Args)
	_, err := hm.file.WriteString(line)
	if err != nil {
		return err
	}

	return hm.file.Sync()
}

func (hm *kvCommandHistoryManager) search(keyword, since, until string, limit int) ([]kvCommandHistory, error) {
	if hm == nil || hm.disabled || hm.file == nil {
		fmt.Println("History is not available.")
		return []kvCommandHistory{}, nil
	}

	now := time.Now()

	// 解析时间范围
	var startTime, endTime int64 = 0, now.Unix()
	if since != "" {
		if t, err := time.ParseInLocation(historyTimeFormat, since, time.Local); err == nil {
			startTime = t.Unix()
		}
	}
	if until != "" {
		if t, err := time.ParseInLocation(historyTimeFormat, until, time.Local); err == nil {
			endTime = t.Unix()
		}
	}

	var results []kvCommandHistory
	scanner := bufio.NewScanner(hm.file)
	scanner.Buffer(make([]byte, 64*1024), maxHistoryRecordSize)

	// 重置文件指针到开始位置
	if _, err := hm.file.Seek(0, 0); err != nil {
		return nil, err
	}

	logger.Debugf("searching history, keyword=%s, since=%s(%d), until=%s(%d), limit=%d", keyword, since, startTime, until, endTime, limit)

	// 读取并过滤记录
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			logger.Debugf("skipped empty line")
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			logger.Debugf("skiped invalid line(expected at least 2 fields): %s", line)
			continue
		}

		timestamp, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			logger.Debugf("skiped invalid timestamp: %s", fields[0])
			continue
		}

		// 时间范围过滤
		if (since != "" && timestamp < startTime) || (until != "" && timestamp > endTime) {
			logger.Debugf("skiped out of range timestamp: %d", timestamp)
			continue
		}

		history := kvCommandHistory{
			Timestamp: timestamp,
			Command:   fields[1],
		}

		if len(fields) > 2 {
			history.Args = strings.Join(fields[2:], " ")
		}

		// 关键词过滤
		if keyword != "" &&
			!strings.Contains(history.Command, keyword) &&
			!strings.Contains(history.Args, keyword) {
			logger.Debugf("skiped non-matching command: %s", line)
			continue
		}

		results = append(results, history)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan history: %w", err)
	}

	// 按时间戳倒序排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].Timestamp > results[j].Timestamp
	})

	// 限制返回条数
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}
