package main

import (
	"bytes"
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yeqown/memcached"
)

func TestCompleteREPLCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		want []string
	}{
		{name: "empty", line: "", want: nil},
		{name: "whitespace", line: "   ", want: nil},
		{name: "known prefix", line: "ge", want: []string{"get", "gets"}},
		{name: "exact command", line: "help", want: []string{"help"}},
		{name: "unknown prefix", line: "zzz", want: []string{}},
		{name: "after first token", line: "get foo", want: nil},
		{name: "trailing space", line: "get ", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := completeREPLCommands(tt.line)
			if tt.want == nil {
				assert.Nil(t, got)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExecuteCommandHelpAndQuit(t *testing.T) {
	t.Parallel()

	repl, err := newREPLCommander(nil, 0)
	require.NoError(t, err)

	help := repl.executeCommand("help")
	assert.False(t, help.quit)
	assert.Contains(t, help.output, "Available commands:")
	assert.Contains(t, help.output, "exit, quit")

	quit := repl.executeCommand("exit")
	assert.True(t, quit.quit)
	assert.Contains(t, quit.output, "Bye!")
}

func TestExecuteCommandEmptyAndUnknown(t *testing.T) {
	t.Parallel()

	repl, err := newREPLCommander(nil, 0)
	require.NoError(t, err)

	empty := repl.executeCommand("   ")
	assert.False(t, empty.quit)
	assert.Empty(t, empty.output)

	unknown := repl.executeCommand("nope")
	assert.False(t, unknown.quit)
	assert.Contains(t, unknown.output, "Unknown command: nope")
}

func TestExecuteCommandWithoutContextReturnsError(t *testing.T) {
	t.Parallel()

	repl, err := newREPLCommander(&contextManager{contexts: map[string]*Context{}}, 0)
	require.NoError(t, err)

	result := repl.executeCommand("get missing")
	assert.False(t, result.quit)
	assert.Contains(t, result.output, "Execution `get` failed: no context selected")
}

func TestIgnoreMemcachedErrorWritesToProvidedWriter(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	err := ignoreMemcachedError(&output, fmt.Errorf("request failed: %w", memcached.ErrNotFound))

	require.NoError(t, err)
	assert.Contains(t, output.String(), "Memcached Error:")
	assert.Contains(t, output.String(), memcached.ErrNotFound.Error())
}

func TestREPLModelEnterRunsCommand(t *testing.T) {
	t.Parallel()

	repl, err := newREPLCommander(nil, 0)
	require.NoError(t, err)
	m := newREPLModel(repl)
	m.input.SetValue("help")

	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)
	updated := next.(replModel)
	assert.True(t, updated.busy)

	msg := cmd()
	result, ok := msg.(commandResultMsg)
	require.True(t, ok)
	assert.Contains(t, result.output, "Available commands:")
	assert.False(t, result.quit)

	next, cmd = updated.Update(result)
	updated = next.(replModel)
	assert.False(t, updated.busy)
	assert.Empty(t, updated.input.Value())
	require.NotNil(t, cmd)
}

func TestREPLModelHistoryNavigation(t *testing.T) {
	t.Parallel()

	repl, err := newREPLCommander(nil, 0)
	require.NoError(t, err)
	m := newREPLModel(repl)
	m.appendHistory("help")
	m.appendHistory("version")
	m.input.SetValue("draft")

	m.historyUp()
	assert.Equal(t, "version", m.input.Value())
	m.historyUp()
	assert.Equal(t, "help", m.input.Value())
	m.historyDown()
	assert.Equal(t, "version", m.input.Value())
	m.historyDown()
	assert.Equal(t, "draft", m.input.Value())
	assert.Equal(t, -1, m.histIdx)
}

func TestREPLModelCtrlCQuits(t *testing.T) {
	t.Parallel()

	repl, err := newREPLCommander(nil, 0)
	require.NoError(t, err)
	m := newREPLModel(repl)

	_, cmd := m.Update(tea.KeyPressMsg{
		Code: 'c',
		Mod:  tea.ModCtrl,
	})
	require.NotNil(t, cmd)
}

func TestREPLModelCtrlDQuitsWhenEmpty(t *testing.T) {
	t.Parallel()

	repl, err := newREPLCommander(nil, 0)
	require.NoError(t, err)
	m := newREPLModel(repl)

	_, cmd := m.Update(tea.KeyPressMsg{
		Code: 'd',
		Mod:  tea.ModCtrl,
	})
	require.NotNil(t, cmd)
}

func TestREPLModelBusyIgnoresTyping(t *testing.T) {
	t.Parallel()

	repl, err := newREPLCommander(nil, 0)
	require.NoError(t, err)
	m := newREPLModel(repl)
	m.busy = true
	m.input.SetValue("help")

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	updated := next.(replModel)
	assert.True(t, updated.busy)
	assert.Equal(t, "help", updated.input.Value())
	assert.Nil(t, cmd)
}

func TestREPLModelExitCommandQuits(t *testing.T) {
	t.Parallel()

	repl, err := newREPLCommander(nil, 0)
	require.NoError(t, err)
	m := newREPLModel(repl)
	m.input.SetValue("quit")

	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated := next.(replModel)
	require.NotNil(t, cmd)
	assert.True(t, updated.busy)

	result := cmd().(commandResultMsg)
	assert.True(t, result.quit)
	assert.Contains(t, result.output, "Bye!")

	_, cmd = updated.Update(result)
	require.NotNil(t, cmd)
}

func TestREPLModelViewBusy(t *testing.T) {
	t.Parallel()

	repl, err := newREPLCommander(nil, 0)
	require.NoError(t, err)
	m := newREPLModel(repl)
	m.busy = true

	view := m.View()
	assert.Contains(t, view.Content, "running")
	assert.False(t, view.AltScreen)
}
