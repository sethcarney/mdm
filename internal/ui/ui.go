package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
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

// minVisible is the minimum number of list rows shown before scrolling kicks in.
const minVisible = 6

// pageSize caps the number of visible list rows so menus stay compact.
const pageSize = 8

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
	offset    int // scroll offset
	height    int // visible terminal rows available for the list
	choice    int
	cancelled bool
	done      bool
}

func (m *selectModel) visibleHeight() int {
	h := m.height - 2 // reserve header + hint rows
	if h < minVisible {
		h = minVisible
	}
	if h > pageSize {
		h = pageSize
	}
	return h
}

func (m *selectModel) clampOffset() {
	vis := m.visibleHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+vis {
		m.offset = m.cursor - vis + 1
	}
	max := len(m.options) - vis
	if max < 0 {
		max = 0
	}
	if m.offset > max {
		m.offset = max
	}
}

func (m *selectModel) Init() tea.Cmd { return nil }

func (m *selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
				m.clampOffset()
			}
		case tea.KeyDown:
			if m.cursor < len(m.options)-1 {
				m.cursor++
				m.clampOffset()
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

	vis := m.visibleHeight()
	end := m.offset + vis
	if end > len(m.options) {
		end = len(m.options)
	}

	if m.offset > 0 {
		sb.WriteString("  " + styleDimmed.Render("↑ more") + "\n")
	}
	for i := m.offset; i < end; i++ {
		opt := m.options[i]
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
	if end < len(m.options) {
		sb.WriteString("  " + styleDimmed.Render("↓ more") + "\n")
	}
	return sb.String()
}

// ─── Select / Confirm ─────────────────────────────────────────────────────────

func UiSelect(message string, options []UIOption) (int, bool) {
	if len(options) == 0 {
		return -1, false
	}
	result, err := tea.NewProgram(&selectModel{message: message, options: options},
		tea.WithAltScreen(),
	).Run()
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
	offset    int
	height    int
	result    []int
	cancelled bool
	done      bool
	required  bool
}

func (m *multiModel) visibleHeight() int {
	// header + hint line + footer hint
	h := m.height - 3
	if h < minVisible {
		h = minVisible
	}
	if h > pageSize {
		h = pageSize
	}
	return h
}

func (m *multiModel) clampOffset() {
	vis := m.visibleHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+vis {
		m.offset = m.cursor - vis + 1
	}
	max := len(m.options) - vis
	if max < 0 {
		max = 0
	}
	if m.offset > max {
		m.offset = max
	}
}

func (m *multiModel) Init() tea.Cmd { return nil }

func handleMultiModelKey(m *multiModel, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
			m.clampOffset()
		}
	case tea.KeyDown:
		if m.cursor < len(m.options)-1 {
			m.cursor++
			m.clampOffset()
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
	if msg.Type == tea.KeySpace || msg.String() == " " {
		if !m.locked[m.cursor] {
			m.selected[m.cursor] = !m.selected[m.cursor]
		}
	}
	return m, nil
}

func (m *multiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
	case tea.KeyMsg:
		return handleMultiModelKey(m, msg)
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

	vis := m.visibleHeight()
	end := m.offset + vis
	if end > len(m.options) {
		end = len(m.options)
	}

	if m.offset > 0 {
		sb.WriteString("  " + styleDimmed.Render("↑ more") + "\n")
	}
	for i := m.offset; i < end; i++ {
		opt := m.options[i]
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
	if end < len(m.options) {
		sb.WriteString("  " + styleDimmed.Render("↓ more") + "\n")
	}
	sb.WriteString(styleDimmed.Render("space to toggle · enter to confirm") + "\n")
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
	}, tea.WithAltScreen()).Run()
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
	input     textinput.Model
	selected  map[int]bool
	filtered  []int
	cursor    int
	offset    int
	height    int
	width     int
	result    []int
	cancelled bool
	done      bool
}

func newSearchModel(message string, options []UIOption, locked []UIOption, selected map[int]bool) *searchModel {
	ti := textinput.New()
	ti.Placeholder = "filter..."
	ti.Focus()
	ti.PromptStyle = styleDimmed
	ti.TextStyle = stylePrompt
	return &searchModel{
		message:  message,
		options:  options,
		locked:   locked,
		input:    ti,
		selected: selected,
		filtered: filterOptions(options, ""),
	}
}

func (m *searchModel) visibleHeight() int {
	// header + search box + footer hint; locked items live in the right panel
	overhead := 3
	h := m.height - overhead
	if h < minVisible {
		h = minVisible
	}
	if h > pageSize {
		h = pageSize
	}
	return h
}

func (m *searchModel) clampOffset() {
	vis := m.visibleHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+vis {
		m.offset = m.cursor - vis + 1
	}
	max := len(m.filtered) - vis
	if max < 0 {
		max = 0
	}
	if m.offset > max {
		m.offset = max
	}
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

func (m *searchModel) Init() tea.Cmd {
	return textinput.Blink
}

func handleSearchModelKey(m *searchModel, msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.Type {
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
			m.clampOffset()
		}
		return m, nil, true
	case tea.KeyDown:
		if len(m.filtered) > 0 && m.cursor < len(m.filtered)-1 {
			m.cursor++
			m.clampOffset()
		}
		return m, nil, true
	case tea.KeyEnter:
		var result []int
		for i, s := range m.selected {
			if s {
				result = append(result, i)
			}
		}
		m.result = result
		m.done = true
		return m, tea.Quit, true
	case tea.KeyEsc, tea.KeyCtrlC:
		m.cancelled = true
		m.done = true
		return m, tea.Quit, true
	}
	// Use msg.String() to detect space reliably across all terminal/platform
	// delivery mechanisms (tea.KeySpace, KeyRunes with ' ', coninput on Windows).
	if msg.Type == tea.KeySpace || msg.String() == " " {
		if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
			fi := m.filtered[m.cursor]
			m.selected[fi] = !m.selected[fi]
		}
		return m, nil, true
	}
	return nil, nil, false
}

func (m *searchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
	case tea.KeyMsg:
		if model, cmd, handled := handleSearchModelKey(m, msg); handled {
			return model, cmd
		}
	}

	// Let textinput handle the keystroke, then sync filtered list.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	newQuery := m.input.Value()
	m.filtered = filterOptions(m.options, newQuery)
	if m.cursor >= len(m.filtered) && len(m.filtered) > 0 {
		m.cursor = len(m.filtered) - 1
	}
	if len(m.filtered) == 0 {
		m.cursor = 0
	}
	m.clampOffset()
	return m, cmd
}

func (m *searchModel) viewOptionsList() string {
	var sb strings.Builder
	vis := m.visibleHeight()
	end := m.offset + vis
	if end > len(m.filtered) {
		end = len(m.filtered)
	}

	if len(m.filtered) == 0 {
		sb.WriteString("  " + styleDimmed.Render("no matches") + "\n")
	} else {
		if m.offset > 0 {
			sb.WriteString("  " + styleDimmed.Render("↑ more") + "\n")
		}
		for idx := m.offset; idx < end; idx++ {
			fi := m.filtered[idx]
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
		if end < len(m.filtered) {
			sb.WriteString("  " + styleDimmed.Render("↓ more") + "\n")
		}
	}
	return sb.String()
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

	footer := styleDimmed.Render("type to filter · space to toggle · enter to confirm")

	if len(m.locked) == 0 {
		header := stylePrompt.Render(m.message) + "\n" + "  " + m.input.View() + "\n"
		return header + m.viewOptionsList() + footer + "\n"
	}

	// Right panel: "always included:" header on the prompt line, items below.
	rightHeader := styleDimmed.Render("always included:")
	var rightItemLines []string
	for _, lo := range m.locked {
		rightItemLines = append(rightItemLines, styleDimmed.Render("◉ "+lo.Label))
	}

	// Measure right panel width.
	rightW := lipgloss.Width(rightHeader)
	for _, rl := range rightItemLines {
		if w := lipgloss.Width(rl); w > rightW {
			rightW = w
		}
	}
	rightW += 2 // breathing room

	// Left column lines: search box, list rows, footer.
	leftContentLines := []string{"  " + m.input.View()}
	leftContentLines = append(leftContentLines, splitLines(m.viewOptionsList())...)
	leftContentLines = append(leftContentLines, footer)

	// Measure left column width (include prompt line itself).
	leftW := lipgloss.Width(stylePrompt.Render(m.message))
	for _, ll := range leftContentLines {
		if w := lipgloss.Width(ll); w > leftW {
			leftW = w
		}
	}
	leftW += 2 // breathing room

	if m.width > 0 && leftW+5+rightW > m.width {
		// Terminal too narrow: single-column fallback.
		var labels []string
		for _, lo := range m.locked {
			labels = append(labels, lo.Label)
		}
		header := stylePrompt.Render(m.message) + "\n" + "  " + m.input.View() + "\n"
		return header + m.viewOptionsList() +
			"  " + styleDimmed.Render("always included: "+strings.Join(labels, ", ")) + "\n" +
			footer + "\n"
	}

	var sb strings.Builder

	// Prompt line: "always included:" header appears to the right.
	sb.WriteString(padRight(stylePrompt.Render(m.message), leftW) + "  " + styleDimmed.Render("│") + "  " + rightHeader + "\n")

	// Zip left content lines with right item lines.
	rows := len(leftContentLines)
	if len(rightItemLines) > rows {
		rows = len(rightItemLines)
	}
	for i := 0; i < rows; i++ {
		left, right := "", ""
		if i < len(leftContentLines) {
			left = leftContentLines[i]
		}
		if i < len(rightItemLines) {
			right = rightItemLines[i]
		}
		if right != "" {
			sb.WriteString(padRight(left, leftW) + "  " + styleDimmed.Render("│") + "  " + right + "\n")
		} else {
			sb.WriteString(left + "\n")
		}
	}
	return sb.String()
}

// splitLines splits a newline-delimited string into a slice, dropping the
// trailing empty element that strings.Split produces for a trailing newline.
func splitLines(s string) []string {
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// padRight pads s with spaces on the right until its visible (ANSI-stripped)
// width reaches the target. Uses lipgloss.Width for accurate measurement.
func padRight(s string, width int) string {
	if vis := lipgloss.Width(s); vis < width {
		return s + strings.Repeat(" ", width-vis)
	}
	return s
}

// ─── SearchMultiselect ─────────────────────────────────────────────────────────

func UiSearchMultiselect(message string, options []UIOption, locked []UIOption, initialSelected []int) ([]int, bool) {
	selected := make(map[int]bool)
	for _, i := range initialSelected {
		if i >= 0 && i < len(options) {
			selected[i] = true
		}
	}
	result, err := tea.NewProgram(
		newSearchModel(message, options, locked, selected),
		tea.WithAltScreen(),
	).Run()
	if err != nil {
		return nil, false
	}
	final := result.(*searchModel)
	if final.cancelled {
		return nil, false
	}
	return final.result, true
}
