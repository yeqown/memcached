package main

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

type commandResultMsg commandResult

type replModel struct {
	repl    *replCommander
	input   textinput.Model
	busy    bool
	history []string
	histIdx int // -1 means browsing is inactive
	draft   string
	width   int
}

func newREPLModel(repl *replCommander) replModel {
	ti := textinput.New()
	ti.Prompt = ">>> "
	ti.ShowSuggestions = true
	ti.SetSuggestions(replCommands)
	ti.SetVirtualCursor(false)
	ti.Focus()
	ti.CharLimit = 0
	ti.SetWidth(80)

	return replModel{
		repl:    repl,
		input:   ti,
		histIdx: -1,
		width:   80,
	}
}

func runREPLTUI(repl *replCommander) error {
	p := tea.NewProgram(newREPLModel(repl))
	_, err := p.Run()
	return err
}

func (m replModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m replModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		if m.width > 4 {
			m.input.SetWidth(m.width - 4)
		}
		return m, nil

	case commandResultMsg:
		m.busy = false
		m.input.SetValue("")
		m.input.Focus()
		cmds := make([]tea.Cmd, 0, 2)
		if msg.output != "" {
			cmds = append(cmds, tea.Println(strings.TrimRight(msg.output, "\n")))
		}
		if msg.quit {
			cmds = append(cmds, tea.Quit)
			return m, tea.Batch(cmds...)
		}
		return m, tea.Batch(cmds...)

	case tea.KeyPressMsg:
		if m.busy {
			if msg.String() == "ctrl+c" {
				return m, tea.Batch(tea.Println("Bye!"), tea.Quit)
			}
			return m, nil
		}

		switch msg.String() {
		case "ctrl+c":
			return m, tea.Batch(tea.Println("Bye!"), tea.Quit)
		case "ctrl+d":
			if m.input.Value() == "" {
				return m, tea.Batch(tea.Println("Bye!"), tea.Quit)
			}
		case "enter":
			line := strings.TrimSpace(m.input.Value())
			if line == "" {
				return m, nil
			}
			m.appendHistory(line)
			m.busy = true
			m.input.Blur()
			return m, m.runCommand(line)
		case "up":
			m.historyUp()
			return m, nil
		case "down":
			m.historyDown()
			return m, nil
		}
	}

	if m.busy {
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m replModel) View() tea.View {
	content := m.input.View()
	if m.busy {
		content = "… running"
	}

	v := tea.NewView(content)
	if !m.busy {
		if c := m.input.Cursor(); c != nil {
			v.Cursor = c
		}
	}
	return v
}

func (m *replModel) appendHistory(line string) {
	if n := len(m.history); n > 0 && m.history[n-1] == line {
		m.histIdx = -1
		m.draft = ""
		return
	}
	m.history = append(m.history, line)
	m.histIdx = -1
	m.draft = ""
}

func (m *replModel) historyUp() {
	if len(m.history) == 0 {
		return
	}
	if m.histIdx == -1 {
		m.draft = m.input.Value()
		m.histIdx = len(m.history) - 1
	} else if m.histIdx > 0 {
		m.histIdx--
	}
	m.input.SetValue(m.history[m.histIdx])
	m.input.CursorEnd()
}

func (m *replModel) historyDown() {
	if m.histIdx == -1 {
		return
	}
	if m.histIdx < len(m.history)-1 {
		m.histIdx++
		m.input.SetValue(m.history[m.histIdx])
		m.input.CursorEnd()
		return
	}
	m.histIdx = -1
	m.input.SetValue(m.draft)
	m.input.CursorEnd()
}

func (m replModel) runCommand(line string) tea.Cmd {
	repl := m.repl
	return func() tea.Msg {
		return commandResultMsg(repl.executeCommand(line))
	}
}
