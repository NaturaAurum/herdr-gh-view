package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	refreshEvery = 60 * time.Second
	panelWidth   = 60 // columns; ponytail: fixed, make configurable if people ask
)

var (
	dim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	bold   = lipgloss.NewStyle().Bold(true)
	green  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	red    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	blue   = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	cursor = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)
)

// line is one rendered row. Rows sharing an item index form one selectable
// entry that opens url.
type line struct {
	text string
	url  string
	item int
}

type (
	debounceMsg struct{ gen int }
	tickMsg     struct{}
	statusMsg   struct {
		seq int
		cwd string
		st  *Status
		err error
	}
)

type model struct {
	self          string
	paneID, cwd   string
	seq, debounce int
	st            *Status
	err           error
	loading       bool
	lines         []line
	items         int
	sel, offset   int
	width, height int
}

func (m *model) fetch() tea.Cmd {
	m.seq++
	m.loading = true
	seq, paneID, cwd := m.seq, m.paneID, m.cwd
	return func() tea.Msg {
		if paneID != "" {
			if c, err := paneCwd(paneID); err == nil && c != "" {
				cwd = c
			}
		}
		st, err := fetchStatus(cwd)
		return statusMsg{seq, cwd, st, err}
	}
}

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *model) Init() tea.Cmd { return tea.Batch(m.fetch(), tick()) }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.build()
	case focusMsg:
		// Clicking into the panel itself must not change what it shows.
		if msg.paneID == "" || msg.paneID == m.self {
			return m, nil
		}
		m.paneID = msg.paneID
		m.debounce++
		gen := m.debounce
		return m, tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg { return debounceMsg{gen} })
	case debounceMsg:
		if msg.gen == m.debounce {
			return m, m.fetch()
		}
	case tickMsg:
		return m, tea.Batch(m.fetch(), tick())
	case statusMsg:
		if msg.seq != m.seq {
			return m, nil
		}
		m.loading = false
		m.cwd, m.st, m.err = msg.cwd, msg.st, msg.err
		m.build()
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "j", "down", "tab":
			m.move(1)
		case "k", "up", "shift+tab":
			m.move(-1)
		case "enter", "o":
			m.open(m.sel)
		case "r":
			return m, m.fetch()
		}
	case tea.MouseMsg:
		switch {
		case msg.Button == tea.MouseButtonWheelDown:
			m.scroll(3)
		case msg.Button == tea.MouseButtonWheelUp:
			m.scroll(-3)
		case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
			if i := m.offset + msg.Y; i < len(m.lines) && m.lines[i].url != "" {
				m.sel = m.lines[i].item
				m.open(m.sel)
			}
		}
	}
	return m, nil
}

func (m *model) bodyHeight() int { return max(m.height-1, 1) }

func (m *model) scroll(d int) {
	m.offset = max(min(m.offset+d, len(m.lines)-m.bodyHeight()), 0)
}

func (m *model) move(d int) {
	if m.items == 0 {
		return
	}
	m.sel = (m.sel + d + m.items) % m.items
	for i, l := range m.lines {
		if l.url != "" && l.item == m.sel {
			if i < m.offset {
				m.offset = i
			} else if i >= m.offset+m.bodyHeight() {
				m.offset = i - m.bodyHeight() + 1
			}
			return
		}
	}
}

func (m *model) open(item int) {
	for _, l := range m.lines {
		if l.url != "" && l.item == item {
			opener := "xdg-open"
			if runtime.GOOS == "darwin" {
				opener = "open"
			}
			exec.Command(opener, l.url).Start()
			return
		}
	}
}

// build renders the status into rows sized for the current width.
func (m *model) build() {
	w := max(m.width-2, 10) // 2 columns of gutter for the selection marker
	m.lines, m.items = nil, 0
	add := func(text, url string) {
		item := -1
		if url != "" {
			// Consecutive rows with the same url are one item.
			if n := len(m.lines); n > 0 && m.lines[n-1].url == url {
				item = m.lines[n-1].item
			} else {
				item = m.items
				m.items++
			}
		}
		m.lines = append(m.lines, line{ansi.Truncate(text, w, "…"), url, item})
	}
	wrap := func(text string, style lipgloss.Style, indent, maxLines int, url string) {
		rows := strings.Split(lipgloss.NewStyle().Width(w-indent).Render(text), "\n")
		for i := range rows {
			rows[i] = strings.TrimRight(rows[i], " ")
		}
		if maxLines > 0 && len(rows) > maxLines {
			rows = rows[:maxLines]
			rows[maxLines-1] += "…"
		}
		for _, r := range rows {
			add(strings.Repeat(" ", indent)+style.Render(r), url)
		}
	}

	if m.err != nil {
		if m.err == errNoRepo {
			add(dim.Render("Not a git repository"), "")
		} else {
			wrap("⚠ "+m.err.Error(), red, 0, 0, "")
		}
		add(dim.Render(m.cwd), "")
		m.finish()
		return
	}
	if m.st == nil {
		add(dim.Render("Loading…"), "")
		m.finish()
		return
	}

	st := m.st
	add(dim.Render(filepath.Base(m.cwd)+" · ")+st.Branch, "")
	add("", "")
	if st.PR == nil {
		add(dim.Render("No open PR for this branch"), "")
		m.finish()
		return
	}

	pr := st.PR
	state := green.Render("● Open")
	if pr.IsDraft {
		state = dim.Render("◌ Draft")
	}
	add(blue.Bold(true).Underline(true).Render(fmt.Sprintf("#%d", pr.Number))+"  "+state, pr.URL)
	wrap(pr.Title, bold, 0, 3, pr.URL)
	add(dim.Render(pr.Head+" → "+pr.Base), "")

	if len(st.Stack) > 0 {
		add("", "")
		add(bold.Render(fmt.Sprintf("Stack (%d)", len(st.Stack))), "")
		// Top of the stack first, like Graphite and gh stack.
		for i := len(st.Stack) - 1; i >= 0; i-- {
			p := st.Stack[i]
			mark, num := "  ", fmt.Sprintf("#%d", p.Number)
			if p.Number == pr.Number {
				mark, num = blue.Render("▶ "), bold.Render(num)
			}
			if p.IsDraft {
				num += dim.Render(" ◌")
			}
			add(mark+num+" "+dim.Render(p.Title), p.URL)
		}
		add(dim.Render("  ↓ "+st.Stack[0].Base), "")
	}

	add("", "")
	add(m.ciLine(), "")
	for _, c := range st.Checks {
		switch c.State {
		case "fail":
			add("  "+red.Render("✗ ")+c.Name, c.URL)
		case "pending":
			add("  "+yellow.Render("● ")+c.Name, c.URL)
		}
	}

	add("", "")
	head := fmt.Sprintf("Review threads (%d)", len(st.Threads))
	if st.Resolved > 0 {
		head += dim.Render(fmt.Sprintf(" +%d resolved", st.Resolved))
	}
	add(bold.Render(head), "")
	for _, t := range st.Threads {
		loc := filepath.Base(t.Path)
		if t.Line > 0 {
			loc += fmt.Sprintf(":%d", t.Line)
		}
		add(blue.Render(loc)+" "+dim.Render(t.Author), t.URL)
		wrap(t.Body, lipgloss.NewStyle(), 2, 2, t.URL)
		if t.Replies > 0 {
			add(dim.Render(fmt.Sprintf("  +%d replies", t.Replies)), t.URL)
		}
	}

	add("", "")
	add(bold.Render(fmt.Sprintf("Comments (%d)", len(st.Comments))), "")
	for i := len(st.Comments) - 1; i >= 0; i-- { // newest first
		c := st.Comments[i]
		add(dim.Render(c.Author), c.URL)
		wrap(c.Body, lipgloss.NewStyle(), 2, 2, c.URL)
	}
	m.finish()
}

func (m *model) finish() {
	if m.sel >= m.items {
		m.sel = max(m.items-1, 0)
	}
	m.scroll(0)
}

func (m *model) ciLine() string {
	var pass, fail, pending, total int
	for _, c := range m.st.Checks {
		switch c.State {
		case "pass":
			pass++
		case "fail":
			fail++
		case "pending":
			pending++
		}
		if c.State != "skip" {
			total++
		}
	}
	counts := dim.Render(fmt.Sprintf(" %d/%d", pass, total))
	switch {
	case total == 0:
		return bold.Render("CI ") + dim.Render("no checks")
	case fail > 0:
		return bold.Render("CI ") + red.Render(fmt.Sprintf("✗ %d failing", fail)) + counts
	case pending > 0:
		return bold.Render("CI ") + yellow.Render(fmt.Sprintf("● %d pending", pending)) + counts
	}
	return bold.Render("CI ") + green.Render("✓ passing") + counts
}

func (m *model) View() string {
	var b strings.Builder
	end := min(m.offset+m.bodyHeight(), len(m.lines))
	for i := m.offset; i < end; i++ {
		l := m.lines[i]
		if l.url != "" && l.item == m.sel {
			b.WriteString(cursor.Render("▌ "))
		} else {
			b.WriteString("  ")
		}
		b.WriteString(l.text + "\n")
	}
	for i := end - m.offset; i < m.bodyHeight(); i++ {
		b.WriteString("\n")
	}
	help := "j/k move · ⏎ open · r refresh · q close"
	if m.loading {
		help = "⟳ " + help
	}
	b.WriteString(dim.Render(ansi.Truncate(help, max(m.width, 1), "…")))
	return b.String()
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "open" {
		if err := openPanel(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	paneID, cwd := initialTarget()
	m := &model{self: os.Getenv("HERDR_PANE_ID"), paneID: paneID, cwd: cwd}
	if m.self != "" {
		go fitWidth(m.self, panelWidth)
	}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	go subscribeFocus(func(f focusMsg) { p.Send(f) })
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
