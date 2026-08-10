package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHistorySearchSupportsRecordsLargerThanScannerDefault(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp(t.TempDir(), "history-*")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })

	value := strings.Repeat("x", 128*1024)
	_, err = file.WriteString("1 set key " + value + "\n")
	require.NoError(t, err)

	manager := &kvCommandHistoryManager{file: file}
	results, err := manager.search("", "", "", 10)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "set", results[0].Command)
	assert.Equal(t, "key "+value, results[0].Args)
}

func TestHistorySearchReportsReadErrors(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp(t.TempDir(), "history-*")
	require.NoError(t, err)
	require.NoError(t, file.Close())

	manager := &kvCommandHistoryManager{file: file}
	_, err = manager.search("", "", "", 10)
	assert.Error(t, err)
}

func TestRecordHistoryWarnsWithoutFailingCommand(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp(t.TempDir(), "history-*")
	require.NoError(t, err)
	require.NoError(t, file.Close())

	var stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&stderr)
	recordHistory(cmd, &kvCommandHistoryManager{file: file}, "set", []string{"key", "value"})

	assert.Contains(t, stderr.String(), "Warning: failed to record set command in history")
}
