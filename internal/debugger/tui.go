package debugger

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yourname/lumen/internal/asm"
	"github.com/yourname/lumen/internal/vm"
)

// ExecutionMode represents the current execution mode
type ExecutionMode int

const (
	ModePaused ExecutionMode = iota
	ModeStep
	ModeContinue
	ModeQuit
)

// Model is the Bubble Tea model for the debugger TUI
type Model struct {
	// VM state
	vm          *vm.VM
	chunk       *vm.Chunk
	disassembly string
	disasmLines []string

	// Debugger state
	mode         ExecutionMode
	currentIP    int
	breakpoints  *BreakpointManager
	history      *ExecutionHistory
	historyIndex int // -1 = live, >= 0 = viewing history

	// UI state
	codeViewport  viewport.Model
	regsViewport  viewport.Model
	stackViewport viewport.Model
	helpText      string
	statusText    string
	width         int
	height        int
	ready         bool

	// Execution control
	shouldPause   bool
	stepRequested bool
	continueExec  bool
	err           error
}

// NewModel creates a new debugger TUI model
func NewModel(v *vm.VM, chunk *vm.Chunk) Model {
	disasm := asm.Disassemble(chunk)
	lines := strings.Split(disasm, "\n")

	return Model{
		vm:            v,
		chunk:         chunk,
		disassembly:   disasm,
		disasmLines:   lines,
		mode:          ModePaused,
		currentIP:     0,
		breakpoints:   NewBreakpointManager(chunk),
		history:       NewExecutionHistory(),
		historyIndex:  -1,
		shouldPause:   true,
		stepRequested: false,
		continueExec:  false,
		helpText:      buildHelpText(),
		statusText:    "Debugger ready. Press 's' to step, 'c' to continue, 'q' to quit.",
	}
}

// Init implements tea.Model
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.mode = ModeQuit
			return m, tea.Quit

		case "s":
			m.mode = ModeStep
			m.stepRequested = true
			m.shouldPause = false
			m.continueExec = false
			m.statusText = "Stepping..."

		case "c":
			m.mode = ModeContinue
			m.shouldPause = false
			m.continueExec = true
			m.statusText = "Continuing..."

		case "b":
			if m.breakpoints.ShouldBreak(m.currentIP) {
				m.breakpoints.RemoveBreakpoint(m.currentIP)
				m.statusText = fmt.Sprintf("Removed breakpoint at IP %d", m.currentIP)
			} else {
				m.breakpoints.AddBreakpoint(m.currentIP)
				m.statusText = fmt.Sprintf("Added breakpoint at IP %d", m.currentIP)
			}

		case "r":
			m.statusText = "Registers view"

		case "h":
			if m.history.Size() > 0 {
				m.statusText = fmt.Sprintf("History: %d snapshots", m.history.Size())
			} else {
				m.statusText = "No execution history yet"
			}

		case "up":
			if m.historyIndex == -1 && m.history.Size() > 0 {
				m.historyIndex = m.history.Size() - 1
			} else if m.historyIndex > 0 {
				m.historyIndex--
			}
			m.updateFromHistory()

		case "down":
			if m.historyIndex >= 0 {
				if m.historyIndex < m.history.Size()-1 {
					m.historyIndex++
				} else {
					m.historyIndex = -1
				}
			}
			m.updateFromHistory()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		if !m.ready {
			m.codeViewport = viewport.New(msg.Width/2-2, msg.Height-10)
			m.regsViewport = viewport.New(msg.Width/2-2, msg.Height/2-5)
			m.stackViewport = viewport.New(msg.Width/2-2, msg.Height/2-5)
			m.ready = true
		} else {
			m.codeViewport.Width = msg.Width/2 - 2
			m.codeViewport.Height = msg.Height - 10
			m.regsViewport.Width = msg.Width/2 - 2
			m.regsViewport.Height = msg.Height/2 - 5
			m.stackViewport.Width = msg.Width/2 - 2
			m.stackViewport.Height = msg.Height/2 - 5
		}

		m.updateViewports()
	}

	m.codeViewport, cmd = m.codeViewport.Update(msg)
	cmds = append(cmds, cmd)

	m.regsViewport, cmd = m.regsViewport.Update(msg)
	cmds = append(cmds, cmd)

	m.stackViewport, cmd = m.stackViewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// View implements tea.Model
func (m Model) View() string {
	if !m.ready {
		return "Initializing debugger..."
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12")).
		Padding(0, 1)

	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(0, 1)

	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		Padding(0, 1)

	codeTitle := titleStyle.Render("Code (IP: " + fmt.Sprintf("%d", m.currentIP) + ")")
	codePanel := panelStyle.Render(m.codeViewport.View())
	leftPanel := lipgloss.JoinVertical(lipgloss.Left, codeTitle, codePanel)

	regsTitle := titleStyle.Render("Registers")
	regsPanel := panelStyle.Render(m.regsViewport.View())
	regsSection := lipgloss.JoinVertical(lipgloss.Left, regsTitle, regsPanel)

	stackTitle := titleStyle.Render("Stack & Heap")
	stackPanel := panelStyle.Render(m.stackViewport.View())
	stackSection := lipgloss.JoinVertical(lipgloss.Left, stackTitle, stackPanel)

	rightPanel := lipgloss.JoinVertical(lipgloss.Left, regsSection, stackSection)

	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	status := statusStyle.Render(m.statusText)

	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8")).
		Render(m.helpText)

	return lipgloss.JoinVertical(lipgloss.Left, mainContent, status, help)
}

// updateViewports updates the content of all viewports
func (m *Model) updateViewports() {
	m.codeViewport.SetContent(m.renderCodeView())
	m.regsViewport.SetContent(m.renderRegistersView())
	m.stackViewport.SetContent(m.renderStackView())
}

// renderCodeView renders the code view with current instruction highlighted
func (m *Model) renderCodeView() string {
	var sb strings.Builder

	highlightStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("12")).
		Foreground(lipgloss.Color("0")).
		Bold(true)

	breakpointStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("9")).
		Bold(true)

	for i, line := range m.disasmLines {
		prefix := "  "

		if strings.Contains(line, fmt.Sprintf("%04d:", m.currentIP)) {
			prefix = "→ "
			line = highlightStyle.Render(line)
		} else if m.breakpoints.ShouldBreak(i) {
			prefix = "● "
			line = breakpointStyle.Render(line)
		}

		sb.WriteString(prefix + line + "\n")
	}

	return sb.String()
}

// renderRegistersView renders the registers view
func (m *Model) renderRegistersView() string {
	var sb strings.Builder

	var regs []vm.Value
	if m.historyIndex >= 0 {
		snapshot := m.history.Get(m.historyIndex)
		if snapshot != nil {
			regs = snapshot.Registers
		}
	} else {
		sb.WriteString("No registers to display (execution not started)\n")
		return sb.String()
	}

	if len(regs) == 0 {
		sb.WriteString("No registers allocated\n")
		return sb.String()
	}

	for i, val := range regs {
		sb.WriteString(fmt.Sprintf("r%d = %s\n", i, InspectValue(val)))
	}

	return sb.String()
}

// renderStackView renders the stack and heap info
func (m *Model) renderStackView() string {
	var sb strings.Builder

	var heapAlloc, heapMax uint64
	var frameCount int

	if m.historyIndex >= 0 {
		snapshot := m.history.Get(m.historyIndex)
		if snapshot != nil {
			heapAlloc = snapshot.HeapAllocated
			heapMax = snapshot.HeapMax
			frameCount = snapshot.FrameCount
		}
	}

	sb.WriteString(fmt.Sprintf("Frame Count: %d\n", frameCount))
	if heapMax > 0 {
		sb.WriteString(fmt.Sprintf("Heap: %d / %d bytes (%.1f%%)\n",
			heapAlloc, heapMax,
			float64(heapAlloc)/float64(heapMax)*100))
	} else {
		sb.WriteString(fmt.Sprintf("Heap: %d bytes\n", heapAlloc))
	}

	sb.WriteString("\nBreakpoints:\n")
	bps := m.breakpoints.List()
	if len(bps) == 0 {
		sb.WriteString("  None\n")
	} else {
		for _, bp := range bps {
			sb.WriteString(fmt.Sprintf("  IP %d\n", bp))
		}
	}

	return sb.String()
}

// updateFromHistory updates the display from history
func (m *Model) updateFromHistory() {
	if m.historyIndex >= 0 {
		snapshot := m.history.Get(m.historyIndex)
		if snapshot != nil {
			m.currentIP = snapshot.IP
			m.statusText = fmt.Sprintf("History [%d/%d]", m.historyIndex+1, m.history.Size())
		}
	} else {
		m.statusText = "Live execution"
	}
	m.updateViewports()
}

// buildHelpText builds the help text
func buildHelpText() string {
	return "s: step | c: continue | b: toggle breakpoint | ↑/↓: navigate history | h: help | q: quit"
}

// OnInstruction is called when an instruction is about to execute
func (m *Model) OnInstruction(snapshot *VMSnapshot) {
	m.history.Add(snapshot)
	m.currentIP = snapshot.IP

	if m.stepRequested {
		m.stepRequested = false
		m.shouldPause = true
	}

	if m.continueExec && m.breakpoints.ShouldBreak(m.currentIP) {
		m.shouldPause = true
		m.continueExec = false
		m.statusText = fmt.Sprintf("Breakpoint hit at IP %d", m.currentIP)
	}

	m.updateViewports()
}

// ShouldContinue returns whether execution should continue
func (m *Model) ShouldContinue() bool {
	return !m.shouldPause
}
