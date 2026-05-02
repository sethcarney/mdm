package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ANSI color/format constants (exported)
const (
	Reset  = "\x1b[0m"
	Bold   = "\x1b[1m"
	Dim    = "\x1b[38;5;243m"
	Text   = "\x1b[38;5;159m"
	Cyan   = "\x1b[36m"
	Yellow = "\x1b[33m"
	Green  = "\x1b[32m"
	Red    = "\x1b[31m"
)

// ANSI sequences used by the Spinner (Bubbletea does not manage spinner output).
const (
	ansiClearLine  = "\033[2K"
	ansiHideCursor = "\033[?25l"
	ansiShowCursor = "\033[?25h"
	ansiCR         = "\r"
)

var (
	stylePrompt = lipgloss.NewStyle().Foreground(lipgloss.Color("159"))
	styleDimmed = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
)

// ─── Log helpers ───────────────────────────────────────────────────────────────

func LogInfo(msg string) {
	fmt.Printf("  %s•%s %s\n", Dim, Reset, msg)
}

func LogSuccess(msg string) {
	fmt.Printf("  %s✓%s %s\n", Text, Reset, msg)
}

func LogWarn(msg string) {
	fmt.Printf("  %s!%s %s\n", Text, Reset, msg)
}

func LogError(msg string) {
	fmt.Fprintf(os.Stderr, "  %s✗%s %s\n", Text, Reset, msg)
}

func Intro(title string) {
	fmt.Printf("\n%s%s%s\n\n", Text, title, Reset)
}

func Outro(message string) {
	fmt.Printf("\n%s%s%s\n\n", Dim, message, Reset)
}

// ─── Note / box ────────────────────────────────────────────────────────────────

func Note(content, title string) {
	lines := strings.Split(content, "\n")
	width := len(title) + 4
	for _, l := range lines {
		if len(l)+4 > width {
			width = len(l) + 4
		}
	}

	border := strings.Repeat("─", width-2)
	if title != "" {
		fmt.Printf("%s┌─ %s%s%s %s─┐%s\n", Dim, Reset, title, Dim, strings.Repeat("─", max(0, width-len(title)-5)), Reset)
	} else {
		fmt.Printf("%s┌%s┐%s\n", Dim, border, Reset)
	}
	for _, l := range lines {
		pad := width - len(l) - 3
		if pad < 0 {
			pad = 0
		}
		fmt.Printf("%s│%s %s%s%s\n", Dim, Reset, l, strings.Repeat(" ", pad), Dim+"│"+Reset)
	}
	fmt.Printf("%s└%s┘%s\n", Dim, border, Reset)
}

// ─── Spinner ───────────────────────────────────────────────────────────────────

type Spinner struct {
	msg  string
	done chan struct{}
}

func NewSpinner(msg string) *Spinner {
	s := &Spinner{msg: msg, done: make(chan struct{})}
	go s.run()
	return s
}

func (s *Spinner) run() {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	i := 0
	fmt.Print(ansiHideCursor)
	for {
		select {
		case <-s.done:
			return
		default:
			fmt.Printf("%s%s%s %s%s", ansiCR, Dim, frames[i%len(frames)], s.msg, Reset)
			i++
			select {
			case <-s.done:
				return
			case <-after(80):
			}
		}
	}
}

func (s *Spinner) Stop(msg string) {
	close(s.done)
	fmt.Print(ansiShowCursor)
	fmt.Printf("%s%s%s %s\n", ansiCR, ansiClearLine, Dim, Reset)
	if msg != "" {
		fmt.Printf("  %s%s%s\n", Dim, msg, Reset)
	}
}

func after(ms int) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		time.Sleep(time.Duration(ms) * time.Millisecond)
		close(ch)
	}()
	return ch
}

// ─── UIOption ──────────────────────────────────────────────────────────────────

type UIOption struct {
	Label string
	Value string
	Hint  string
}

// ─── selectModel ───────────────────────────────────────────────────────────────

type selectModel struct {
	message   string
	options   []UIOption
	cursor    int
	choice    int
	cancelled bool
	done      bool
}

func (m *selectModel) Init() tea.Cmd { return nil }

func (m *selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.KeyDown:
			if m.cursor < len(m.options)-1 {
				m.cursor++
			}
		case tea.KeyEnter:
			m.choice = m.cursor
			m.done = true
			return m, tea.Quit
		case tea.KeyEsc, tea.KeyCtrlC:
			m.cancelled = true
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *selectModel) View() string {
	if m.done {
		if m.cancelled {
			return styleDimmed.Render(m.message) + "\n"
		}
		return stylePrompt.Render(m.message) + "  " + styleDimmed.Render(m.options[m.choice].Label) + "\n"
	}
	var sb strings.Builder
	sb.WriteString(stylePrompt.Render(m.message) + "\n")
	for i, opt := range m.options {
		if i == m.cursor {
			sb.WriteString("  " + stylePrompt.Render("❯") + " " + opt.Label)
		} else {
			sb.WriteString("    " + styleDimmed.Render(opt.Label))
		}
		if opt.Hint != "" {
			sb.WriteString("  " + styleDimmed.Render(opt.Hint))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// ─── Select / Confirm ─────────────────────────────────────────────────────────

func UiSelect(message string, options []UIOption) (int, bool) {
	if len(options) == 0 {
		return -1, false
	}
	result, err := tea.NewProgram(&selectModel{message: message, options: options}).Run()
	if err != nil {
		return -1, false
	}
	final := result.(*selectModel)
	if final.cancelled {
		return -1, false
	}
	return final.choice, true
}

func UiConfirm(message string) (bool, bool) {
	idx, ok := UiSelect(message, []UIOption{{Label: "Yes"}, {Label: "No"}})
	if !ok {
		return false, false
	}
	return idx == 0, true
}

// ─── multiModel ────────────────────────────────────────────────────────────────

type multiModel struct {
	message   string
	options   []UIOption
	selected  []bool
	locked    map[int]bool
	cursor    int
	result    []int
	cancelled bool
	done      bool
	required  bool
}

func (m *multiModel) Init() tea.Cmd { return nil }

func (m *multiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.KeyDown:
			if m.cursor < len(m.options)-1 {
				m.cursor++
			}
		case tea.KeySpace:
			if !m.locked[m.cursor] {
				m.selected[m.cursor] = !m.selected[m.cursor]
			}
		case tea.KeyEnter:
			var result []int
			for i, s := range m.selected {
				if s {
					result = append(result, i)
				}
			}
			if m.required && len(result) == 0 {
				return m, nil
			}
			m.result = result
			m.done = true
			return m, tea.Quit
		case tea.KeyEsc, tea.KeyCtrlC:
			m.cancelled = true
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *multiModel) View() string {
	if m.done {
		if m.cancelled {
			return styleDimmed.Render(m.message) + "\n"
		}
		var labels []string
		for _, i := range m.result {
			labels = append(labels, m.options[i].Label)
		}
		return stylePrompt.Render(m.message) + "  " + styleDimmed.Render(strings.Join(labels, ", ")) + "\n"
	}
	var sb strings.Builder
	sb.WriteString(stylePrompt.Render(m.message) + "\n")
	for i, opt := range m.options {
		isLocked := m.locked[i]
		checkbox := "○"
		if m.selected[i] {
			checkbox = "●"
		}
		if isLocked {
			checkbox = "◉"
		}
		if i == m.cursor {
			sb.WriteString("  " + stylePrompt.Render("❯") + " " + stylePrompt.Render(checkbox) + " " + opt.Label)
		} else {
			sb.WriteString("    " + styleDimmed.Render(checkbox) + " " + styleDimmed.Render(opt.Label))
		}
		if opt.Hint != "" {
			sb.WriteString("  " + styleDimmed.Render(opt.Hint))
		}
		sb.WriteString("\n")
	}
	sb.WriteString(styleDimmed.Render("space to toggle, enter to confirm") + "\n")
	return sb.String()
}

// ─── Multiselect ───────────────────────────────────────────────────────────────

func UiMultiselect(message string, options []UIOption, required bool, initialSelected []int, locked []int) ([]int, bool) {
	if len(options) == 0 {
		return nil, false
	}
	selected := make([]bool, len(options))
	for _, i := range initialSelected {
		if i >= 0 && i < len(options) {
			selected[i] = true
		}
	}
	lockedSet := make(map[int]bool)
	for _, i := range locked {
		lockedSet[i] = true
		if i >= 0 && i < len(options) {
			selected[i] = true
		}
	}
	result, err := tea.NewProgram(&multiModel{
		message:  message,
		options:  options,
		selected: selected,
		locked:   lockedSet,
		required: required,
	}).Run()
	if err != nil {
		return nil, false
	}
	final := result.(*multiModel)
	if final.cancelled {
		return nil, false
	}
	return final.result, true
}

// ─── searchModel ───────────────────────────────────────────────────────────────

type searchModel struct {
	message   string
	options   []UIOption
	locked    []UIOption
	query     string
	selected  map[int]bool
	filtered  []int
	cursor    int
	result    []int
	cancelled bool
	done      bool
}

func filterOptions(options []UIOption, query string) []int {
	q := strings.ToLower(query)
	var indices []int
	for i, o := range options {
		if q == "" || strings.Contains(strings.ToLower(o.Label), q) || strings.Contains(strings.ToLower(o.Hint), q) {
			indices = append(indices, i)
		}
	}
	return indices
}

func (m *searchModel) Init() tea.Cmd { return nil }

func (m *searchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.KeyDown:
			if len(m.filtered) > 0 && m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
		case tea.KeySpace:
			if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				fi := m.filtered[m.cursor]
				m.selected[fi] = !m.selected[fi]
			}
		case tea.KeyBackspace, tea.KeyDelete:
			if len(m.query) > 0 {
				m.query = m.query[:len(m.query)-1]
				m.filtered = filterOptions(m.options, m.query)
				if m.cursor >= len(m.filtered) && len(m.filtered) > 0 {
					m.cursor = len(m.filtered) - 1
				}
			}
		case tea.KeyEnter:
			var result []int
			for i, s := range m.selected {
				if s {
					result = append(result, i)
				}
			}
			m.result = result
			m.done = true
			return m, tea.Quit
		case tea.KeyEsc, tea.KeyCtrlC:
			m.cancelled = true
			m.done = true
			return m, tea.Quit
		case tea.KeyRunes:
			m.query += string(msg.Runes)
			m.filtered = filterOptions(m.options, m.query)
			if m.cursor >= len(m.filtered) && len(m.filtered) > 0 {
				m.cursor = len(m.filtered) - 1
			}
		}
	}
	return m, nil
}

func (m *searchModel) View() string {
	if m.done {
		if m.cancelled {
			return styleDimmed.Render(m.message) + "\n"
		}
		var labels []string
		for _, lo := range m.locked {
			labels = append(labels, lo.Label)
		}
		for _, i := range m.result {
			labels = append(labels, m.options[i].Label)
		}
		return stylePrompt.Render(m.message) + "  " + styleDimmed.Render(strings.Join(labels, ", ")) + "\n"
	}
	var sb strings.Builder
	sb.WriteString(stylePrompt.Render(m.message) + "\n")
	sb.WriteString("  " + styleDimmed.Render("[") + m.query + styleDimmed.Render("]") + "\n")
	if len(m.locked) > 0 {
		sb.WriteString("  " + styleDimmed.Render("── always included ──") + "\n")
		for _, lo := range m.locked {
			sb.WriteString("    " + styleDimmed.Render("◉") + " " + styleDimmed.Render(lo.Label))
			if lo.Hint != "" {
				sb.WriteString("  " + styleDimmed.Render(lo.Hint))
			}
			sb.WriteString("\n")
		}
	}
	if len(m.filtered) == 0 {
		sb.WriteString("  " + styleDimmed.Render("no matches") + "\n")
	} else {
		for idx, fi := range m.filtered {
			opt := m.options[fi]
			checkbox := "○"
			if m.selected[fi] {
				checkbox = "●"
			}
			if idx == m.cursor {
				sb.WriteString("  " + stylePrompt.Render("❯") + " " + stylePrompt.Render(checkbox) + " " + opt.Label)
			} else {
				sb.WriteString("    " + styleDimmed.Render(checkbox) + " " + styleDimmed.Render(opt.Label))
			}
			if opt.Hint != "" {
				sb.WriteString("  " + styleDimmed.Render(opt.Hint))
			}
			sb.WriteString("\n")
		}
	}
	sb.WriteString(styleDimmed.Render("type to filter, space to toggle, enter to confirm") + "\n")
	return sb.String()
}

// ─── SearchMultiselect ─────────────────────────────────────────────────────────

func UiSearchMultiselect(message string, options []UIOption, locked []UIOption, initialSelected []int) ([]int, bool) {
	selected := make(map[int]bool)
	for _, i := range initialSelected {
		if i >= 0 && i < len(options) {
			selected[i] = true
		}
	}
	result, err := tea.NewProgram(&searchModel{
		message:  message,
		options:  options,
		locked:   locked,
		selected: selected,
		filtered: filterOptions(options, ""),
	}).Run()
	if err != nil {
		return nil, false
	}
	final := result.(*searchModel)
	if final.cancelled {
		return nil, false
	}
	return final.result, true
}
