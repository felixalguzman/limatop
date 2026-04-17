package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/felixalguzman/limatop/internal/lima"
	"github.com/felixalguzman/limatop/internal/theme"
)

type View int

const (
	ViewTable View = iota
	ViewGrid
	ViewFocus
)

const refreshEvery = 3 * time.Second

// ---- messages ----

type tickMsg time.Time

type vmsMsg struct {
	vms []lima.VM
	err error
}

type procsMsg struct {
	vm    string
	procs []lima.Process
	err   error
}

type usageMsg struct {
	vm    string
	usage lima.Usage
	err   error
}

type actionDoneMsg struct {
	vm     string
	action string
	err    error
}

type shellDoneMsg struct {
	err error
}

// ---- model ----

type Model struct {
	themes   []theme.Theme
	themeIdx int
	view     View

	vms      []lima.VM
	selected int

	procs    []lima.Process
	procsErr error

	usage       map[string]lima.Usage
	usageErr    map[string]error
	cpuHistory  map[string][]float64 // ring of recent CPU% per VM (newest last)
	memHistory  map[string][]float64
	diskHistory map[string][]float64
	netRxRate   map[string]float64 // current rx bytes/sec (for display)
	netTxRate   map[string]float64
	netHistory  map[string][]float64 // combined rx+tx rate sparkline (bytes/sec)
	netPrev     map[string]netSnap   // last cumulative counter snapshot

	width  int
	height int

	loading bool
	err     error

	// Transient status from the most recent start/stop action.
	action    string // e.g. "starting default…"
	actionMsg string // short result, e.g. "started default" or "stop failed: …"
	actionErr bool
	busy      map[string]string // vm -> in-flight action verb ("start"/"stop"/"delete")

	// When non-empty, a modal asks the user to confirm deleting this VM.
	confirmDelete string
}

type netSnap struct {
	rx, tx int64
	at     time.Time
}

const sparkSamples = 200 // ring-buffer length per VM (≈10 min at refreshEvery=3s)

func New() Model {
	return Model{
		themes:      theme.All(),
		view:        ViewTable,
		loading:     true,
		usage:       map[string]lima.Usage{},
		usageErr:    map[string]error{},
		cpuHistory:  map[string][]float64{},
		memHistory:  map[string][]float64{},
		diskHistory: map[string][]float64{},
		netRxRate:   map[string]float64{},
		netTxRate:   map[string]float64{},
		netHistory:  map[string][]float64{},
		netPrev:     map[string]netSnap{},
		busy:        map[string]string{},
	}
}

func pushSample(ring []float64, v float64) []float64 {
	ring = append(ring, v)
	if len(ring) > sparkSamples {
		ring = ring[len(ring)-sparkSamples:]
	}
	return ring
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(fetchVMs(), tick())
}

// ---- commands ----

func fetchVMs() tea.Cmd {
	return func() tea.Msg {
		vms, err := lima.List()
		return vmsMsg{vms: vms, err: err}
	}
}

func fetchProcs(name string) tea.Cmd {
	return func() tea.Msg {
		p, err := lima.Processes(name)
		return procsMsg{vm: name, procs: p, err: err}
	}
}

func fetchUsage(name string) tea.Cmd {
	return func() tea.Msg {
		u, err := lima.GuestUsage(name)
		return usageMsg{vm: name, usage: u, err: err}
	}
}

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func startVM(name string) tea.Cmd {
	return func() tea.Msg {
		return actionDoneMsg{vm: name, action: "start", err: lima.Start(name)}
	}
}

func stopVM(name string) tea.Cmd {
	return func() tea.Msg {
		return actionDoneMsg{vm: name, action: "stop", err: lima.Stop(name)}
	}
}

func shellVM(name string) tea.Cmd {
	return tea.ExecProcess(lima.ShellCmd(name), func(err error) tea.Msg {
		return shellDoneMsg{err: err}
	})
}

func deleteVM(name string) tea.Cmd {
	return func() tea.Msg {
		return actionDoneMsg{vm: name, action: "delete", err: lima.Delete(name)}
	}
}

func pastTense(verb string) string {
	switch verb {
	case "start":
		return "started"
	case "stop":
		return "stopped"
	case "delete":
		return "deleted"
	}
	return verb + "ed"
}

// ---- update ----

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		return m.handleKey(msg)

	case vmsMsg:
		firstLoad := m.loading
		m.loading = false
		m.err = msg.err
		m.vms = msg.vms
		if m.selected >= len(m.vms) {
			m.selected = len(m.vms) - 1
		}
		if m.selected < 0 {
			m.selected = 0
		}
		var cmds []tea.Cmd
		if firstLoad {
			for _, vm := range m.vms {
				if vm.Status == "Running" {
					cmds = append(cmds, fetchUsage(vm.Name))
				}
			}
		}
		if m.view == ViewFocus {
			if vm := m.selectedVM(); vm != nil && vm.Status == "Running" {
				cmds = append(cmds, fetchProcs(vm.Name))
			}
		}
		if len(cmds) > 0 {
			return m, tea.Batch(cmds...)
		}

	case procsMsg:
		if vm := m.selectedVM(); vm != nil && vm.Name == msg.vm {
			m.procs = msg.procs
			m.procsErr = msg.err
		}

	case usageMsg:
		if msg.err != nil {
			m.usageErr[msg.vm] = msg.err
		} else {
			m.usage[msg.vm] = msg.usage
			delete(m.usageErr, msg.vm)
			m.cpuHistory[msg.vm] = pushSample(m.cpuHistory[msg.vm], msg.usage.CPUPct)
			m.memHistory[msg.vm] = pushSample(m.memHistory[msg.vm], msg.usage.MemPct)
			m.diskHistory[msg.vm] = pushSample(m.diskHistory[msg.vm], msg.usage.DiskPct)

			now := time.Now()
			if prev, ok := m.netPrev[msg.vm]; ok {
				elapsed := now.Sub(prev.at).Seconds()
				if elapsed > 0 {
					rx := float64(msg.usage.NetRxBytes-prev.rx) / elapsed
					tx := float64(msg.usage.NetTxBytes-prev.tx) / elapsed
					if rx < 0 {
						rx = 0
					}
					if tx < 0 {
						tx = 0
					}
					m.netRxRate[msg.vm] = rx
					m.netTxRate[msg.vm] = tx
					m.netHistory[msg.vm] = pushSample(m.netHistory[msg.vm], rx+tx)
				}
			}
			m.netPrev[msg.vm] = netSnap{rx: msg.usage.NetRxBytes, tx: msg.usage.NetTxBytes, at: now}
		}

	case tickMsg:
		cmds := []tea.Cmd{fetchVMs(), tick()}
		for _, vm := range m.vms {
			if vm.Status == "Running" {
				cmds = append(cmds, fetchUsage(vm.Name))
			}
		}
		if m.view == ViewFocus {
			if vm := m.selectedVM(); vm != nil && vm.Status == "Running" {
				cmds = append(cmds, fetchProcs(vm.Name))
			}
		}
		return m, tea.Batch(cmds...)

	case actionDoneMsg:
		delete(m.busy, msg.vm)
		m.action = ""
		if msg.err != nil {
			m.actionMsg = fmt.Sprintf("%s %s failed: %s", msg.action, msg.vm, msg.err)
			m.actionErr = true
		} else {
			m.actionMsg = fmt.Sprintf("%s %s", pastTense(msg.action), msg.vm)
			m.actionErr = false
		}
		return m, fetchVMs()

	case shellDoneMsg:
		if msg.err != nil {
			m.actionMsg = "shell exited: " + msg.err.Error()
			m.actionErr = true
		} else {
			m.actionMsg = ""
			m.actionErr = false
		}
		return m, fetchVMs()
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirmDelete != "" {
		switch msg.String() {
		case "y", "Y":
			name := m.confirmDelete
			m.confirmDelete = ""
			m.busy[name] = "delete"
			m.action = "deleting " + name + "…"
			m.actionMsg = ""
			return m, deleteVM(name)
		case "n", "N", "esc", "q", "ctrl+c":
			m.confirmDelete = ""
		}
		return m, nil
	}
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.selected < len(m.vms)-1 {
			m.selected++
		}
	case "k", "up":
		if m.selected > 0 {
			m.selected--
		}
	case "g", "home":
		m.selected = 0
	case "G", "end":
		m.selected = len(m.vms) - 1
		if m.selected < 0 {
			m.selected = 0
		}
	case "r":
		return m, fetchVMs()
	case "t":
		if len(m.themes) > 1 {
			m.themeIdx = (m.themeIdx + 1) % len(m.themes)
		}
	case "v":
		m.view = (m.view + 1) % 3
		return m, m.focusCmdIfNeeded()
	case "1":
		m.view = ViewTable
	case "2":
		m.view = ViewGrid
	case "3":
		m.view = ViewFocus
		return m, m.focusCmdIfNeeded()
	case "enter":
		m.view = ViewFocus
		return m, m.focusCmdIfNeeded()
	case "esc":
		if m.view == ViewFocus {
			m.view = ViewTable
		}
	case "b":
		vm := m.selectedVM()
		if vm == nil {
			return m, nil
		}
		if _, inFlight := m.busy[vm.Name]; inFlight {
			return m, nil
		}
		if vm.Status == "Running" || vm.Status == "Starting" {
			m.actionMsg = vm.Name + " is already " + strings.ToLower(vm.Status)
			m.actionErr = false
			return m, nil
		}
		m.busy[vm.Name] = "start"
		m.action = "starting " + vm.Name + "…"
		m.actionMsg = ""
		return m, startVM(vm.Name)
	case "h":
		vm := m.selectedVM()
		if vm == nil {
			return m, nil
		}
		if _, inFlight := m.busy[vm.Name]; inFlight {
			return m, nil
		}
		if vm.Status != "Running" {
			m.actionMsg = vm.Name + " is not running"
			m.actionErr = false
			return m, nil
		}
		m.busy[vm.Name] = "stop"
		m.action = "stopping " + vm.Name + "…"
		m.actionMsg = ""
		return m, stopVM(vm.Name)
	case "e":
		vm := m.selectedVM()
		if vm == nil {
			return m, nil
		}
		if vm.Status != "Running" {
			m.actionMsg = "cannot shell into " + strings.ToLower(vm.Status) + " vm"
			m.actionErr = true
			return m, nil
		}
		m.actionMsg = ""
		return m, shellVM(vm.Name)
	case "d":
		vm := m.selectedVM()
		if vm == nil {
			return m, nil
		}
		if _, inFlight := m.busy[vm.Name]; inFlight {
			return m, nil
		}
		m.confirmDelete = vm.Name
		m.actionMsg = ""
	}
	return m, nil
}

func (m Model) focusCmdIfNeeded() tea.Cmd {
	if m.view != ViewFocus {
		return nil
	}
	vm := m.selectedVM()
	if vm == nil || vm.Status != "Running" {
		return nil
	}
	return tea.Batch(fetchProcs(vm.Name), fetchUsage(vm.Name))
}

func (m Model) selectedVM() *lima.VM {
	if m.selected < 0 || m.selected >= len(m.vms) {
		return nil
	}
	return &m.vms[m.selected]
}

func (m Model) theme() theme.Theme {
	return m.themes[m.themeIdx]
}

// ---- view ----

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	s := m.styles()

	header := m.renderHeader(s)
	footer := m.renderFooter(s)

	const sidePad = 2
	innerW := m.width - 2*sidePad
	if innerW < 10 {
		innerW = 10
	}

	bodyHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer) - 2
	if bodyHeight < 3 {
		bodyHeight = 3
	}

	var body string
	switch {
	case m.err != nil:
		body = s.Error.Render("error: " + m.err.Error())
	case m.loading:
		body = s.Muted.Render("loading lima VMs…")
	case len(m.vms) == 0:
		body = s.Muted.Render("no lima VMs found.\n\nrun `limactl start` to create one.")
	case m.confirmDelete != "":
		body = m.renderConfirmDelete(s, innerW, bodyHeight)
	default:
		switch m.view {
		case ViewTable:
			body = m.renderTableWithDetail(s, innerW, bodyHeight)
		case ViewGrid:
			body = m.renderGrid(s, innerW)
		case ViewFocus:
			body = m.renderFocus(s, innerW, bodyHeight)
		}
	}

	bodyBlock := lipgloss.NewStyle().
		Width(m.width).
		Height(bodyHeight).
		PaddingTop(1).
		PaddingLeft(sidePad).
		PaddingRight(sidePad).
		Render(body)

	return lipgloss.JoinVertical(lipgloss.Left, header, bodyBlock, footer)
}

// ---- header / footer ----

func (m Model) renderHeader(s styles) string {
	th := m.theme()
	title := s.Title.Render(" limatop ")
	meta := s.HeaderMeta.Render(fmt.Sprintf(" %s · %s · %d vm%s ",
		viewName(m.view), th.Name, len(m.vms), plural(len(m.vms))))

	left := lipgloss.JoinHorizontal(lipgloss.Top, title, meta)
	status := ""
	switch {
	case m.action != "":
		status = s.Warning.Render(" " + m.action + " ")
	case m.actionMsg != "" && m.actionErr:
		status = s.Error.Render(" " + m.actionMsg + " ")
	case m.actionMsg != "":
		status = s.Success.Render(" " + m.actionMsg + " ")
	}
	right := status + s.HeaderMeta.Render(fmt.Sprintf(" %s ", time.Now().Format("15:04:05")))

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	bar := left + strings.Repeat(" ", gap) + right
	return s.HeaderBar.Width(m.width).Render(bar)
}

func (m Model) renderFooter(s styles) string {
	if m.confirmDelete != "" {
		hints := [][2]string{
			{"y", "confirm delete"}, {"n/esc", "cancel"},
		}
		var parts []string
		for _, h := range hints {
			cap := s.KeyCap
			if h[0] == "y" {
				cap = s.KeyCapDanger
			}
			parts = append(parts, cap.Render(h[0])+" "+s.Muted.Render(h[1]))
		}
		hint := strings.Join(parts, s.Muted.Render(" · "))
		return s.FooterBar.Width(m.width).Render(" " + hint + " ")
	}
	hints := []struct {
		key, label string
		danger     bool
	}{
		{"j/k", "move", false}, {"enter", "focus", false}, {"v", "view", false},
		{"b", "boot", false}, {"h", "halt", false}, {"e", "shell", false},
		{"d", "delete", true},
		{"r", "refresh", false}, {"t", "theme", false}, {"q", "quit", false},
	}
	var parts []string
	for _, h := range hints {
		cap := s.KeyCap
		if h.danger {
			cap = s.KeyCapDanger
		}
		parts = append(parts, cap.Render(h.key)+" "+s.Muted.Render(h.label))
	}
	hint := strings.Join(parts, s.Muted.Render(" · "))
	return s.FooterBar.Width(m.width).Render(" " + hint + " ")
}

func (m Model) renderConfirmDelete(s styles, innerW, height int) string {
	name := m.confirmDelete
	lines := []string{
		s.Error.Bold(true).Render("Delete VM?"),
		"",
		"This will permanently remove " + s.Value.Bold(true).Render(name) + ".",
		s.Muted.Render("All disks and config for this VM will be destroyed."),
		"",
		s.KeyCapDanger.Render("y") + " " + s.Muted.Render("confirm") +
			"    " +
			s.KeyCap.Render("n/esc") + " " + s.Muted.Render("cancel"),
	}
	modal := s.ConfirmCard.Render(strings.Join(lines, "\n"))
	return lipgloss.Place(innerW, height, lipgloss.Center, lipgloss.Center, modal)
}

func viewName(v View) string {
	switch v {
	case ViewTable:
		return "table"
	case ViewGrid:
		return "grid"
	case ViewFocus:
		return "focus"
	}
	return ""
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// ---- table view ----

func (m Model) renderTable(s styles, innerW int) string {
	// width = minimum width; flex = weight for distributing surplus space.
	// flex==0 means fixed-width.
	cols := []struct {
		name  string
		width int
		flex  int
	}{
		{"", 2, 0},
		{"NAME", 12, 2},
		{"STATUS", 10, 0},
		{"CPU%", 12, 3},
		{"MEM%", 12, 3},
		{"NET", 12, 3},
		{"CPU", 5, 0},
		{"MEM", 10, 0},
		{"DISK", 16, 2},
		{"OS", 7, 0},
		{"ARCH", 9, 0},
		{"TYPE", 6, 0},
		{"SSH", 14, 2},
	}

	// Each row is prefixed with a 2-char selection gutter; between columns
	// there's a single space separator, so each column effectively consumes
	// width+1 cells.
	const gutter = 2
	total := 0
	totalFlex := 0
	for _, c := range cols {
		total += c.width + 1
		totalFlex += c.flex
	}
	budget := innerW - gutter
	switch {
	case total < budget && totalFlex > 0:
		// Distribute surplus across flex columns by weight.
		surplus := budget - total
		for i := range cols {
			if cols[i].flex == 0 {
				continue
			}
			add := surplus * cols[i].flex / totalFlex
			cols[i].width += add
		}
		// Any remainder from integer division goes to the widest-flex column.
		assigned := 0
		for _, c := range cols {
			assigned += c.width + 1
		}
		if leftover := budget - assigned; leftover > 0 {
			for i := range cols {
				if cols[i].flex > 0 {
					cols[i].width += leftover
					break
				}
			}
		}
	case total > budget:
		// Shrink from the tail until we fit.
		overflow := total - budget
		for i := len(cols) - 1; i >= 0 && overflow > 0; i-- {
			if cols[i].flex == 0 && cols[i].name != "SSH" {
				continue
			}
			shrink := cols[i].width - 3
			if shrink > overflow {
				shrink = overflow
			}
			if shrink > 0 {
				cols[i].width -= shrink
				overflow -= shrink
			}
		}
	}

	var header strings.Builder
	// Leading space for the selection bar.
	header.WriteString("  ")
	for _, c := range cols {
		header.WriteString(s.TableHeader.Width(c.width).Render(c.name))
		header.WriteString(" ")
	}

	divider := "  " + s.Muted.Render(strings.Repeat("─", innerW-2))

	rows := []string{header.String(), divider}

	th := m.theme()
	for i, vm := range m.vms {
		selected := i == m.selected
		cells := []string{
			statusDot(s, vm.Status),
			vm.Name,
			statusLabel(s, vm.Status),
			m.sparkCell(vm, m.cpuHistory[vm.Name], th.Accent, th, cols[3].width),
			m.sparkCell(vm, m.memHistory[vm.Name], th.Info, th, cols[4].width),
			m.netCell(vm, th, cols[5].width),
			intOrDash(vm.CPUs),
			bytesOrDash(vm.Memory),
			diskUsage(vm, m.usage[vm.Name]),
			orDash(vm.Config.OS),
			orDash(vm.Arch),
			orDash(vm.VMType),
			sshTarget(vm),
		}

		var row strings.Builder
		if selected {
			row.WriteString(s.RowSelectedBar.Render("▌"))
			row.WriteString(" ")
		} else {
			row.WriteString("  ")
		}
		rowStyle := s.Row
		if selected {
			rowStyle = s.RowSelected
		}
		for idx, c := range cols {
			cell := cells[idx]
			switch idx {
			case 0:
				row.WriteString(lipgloss.NewStyle().Width(c.width).Render(cell))
			case 2, 3, 4, 5:
				// status label + CPU%/MEM%/NET sparklines carry their own color
				row.WriteString(lipgloss.NewStyle().Width(c.width).Render(truncate(cell, c.width)))
			default:
				row.WriteString(rowStyle.Width(c.width).Render(truncate(cell, c.width)))
			}
			row.WriteString(" ")
		}
		rows = append(rows, row.String())
	}

	return strings.Join(rows, "\n")
}

// renderTableWithDetail stacks the scan-friendly table on top and a
// btop-style "what's the selected VM doing" panel below. The detail panel is
// only drawn when there's at least ~8 lines of slack after the table.
func (m Model) renderTableWithDetail(s styles, innerW, bodyHeight int) string {
	table := m.renderTable(s, innerW)
	used := lipgloss.Height(table)
	slack := bodyHeight - used - 2 // 2 for spacer + card outer rows
	if slack < 8 {
		return table
	}
	vm := m.selectedVM()
	if vm == nil {
		return table
	}
	detail := m.renderSelectedDetail(s, innerW, slack, vm)
	return lipgloss.JoinVertical(lipgloss.Left, table, "", detail)
}

func (m Model) renderSelectedDetail(s styles, innerW, height int, vm *lima.VM) string {
	th := m.theme()
	lgw := innerW - borderOnly
	contentW := innerW - cardHFrame
	if contentW < 30 {
		contentW = 30
	}

	title := s.FocusTitle.Render(vm.Name) + " " + statusLabel(s, vm.Status)
	if vm.Status != "Running" {
		body := strings.Join([]string{
			title,
			"",
			s.Muted.Render("select a running VM to see live CPU and memory history."),
		}, "\n")
		return s.FocusCard.Width(lgw).Render(body)
	}

	// Three charts side by side, each with its own axis gutter.
	const gap = 2
	const axisW = 5
	chartH := height - 6 // border(2) + padding(2) + title(1) + blank(1) + padding
	if chartH > 8 {
		chartH = 8
	}
	if chartH < 3 {
		chartH = 3
	}

	thirdW := (contentW - 2*gap) / 3
	plotW := thirdW - axisW
	if plotW < 8 {
		plotW = 8
	}

	cpuHist := m.cpuHistory[vm.Name]
	memHist := m.memHistory[vm.Name]
	netHist := m.netHistory[vm.Name]

	cpuChart := composeChart(s, "CPU", th.Accent, cpuHist, plotW, chartH)
	memChart := composeChart(s, "MEM", th.Info, memHist, plotW, chartH)
	netHeader := "NET"
	if rate := m.netRxRate[vm.Name] + m.netTxRate[vm.Name]; rate > 0 {
		netHeader = fmt.Sprintf("NET %s", humanRate(rate))
	}
	netChart := composeChart(s, netHeader, th.Success, scaleToPeak(netHist), plotW, chartH)

	charts := lipgloss.JoinHorizontal(lipgloss.Top,
		cpuChart, strings.Repeat(" ", gap),
		memChart, strings.Repeat(" ", gap),
		netChart,
	)

	body := strings.Join([]string{title, "", charts}, "\n")
	return s.FocusCard.Width(lgw).Render(body)
}

// composeChart returns a labeled braille chart block with a current/peak header
// and a 0/50/100% axis on the left.
func composeChart(s styles, label string, color lipgloss.Color, hist []float64, plotW, chartH int) string {
	header := s.ProcessTitle.Render(label)
	meta := s.Muted.Render("…")
	if len(hist) > 0 {
		cur := hist[len(hist)-1]
		peak := 0.0
		for _, v := range hist {
			if v > peak {
				peak = v
			}
		}
		meta = s.Muted.Render(fmt.Sprintf("now %5.1f%%   peak %5.1f%%", cur, peak))
	}
	gap := plotW + 5 - lipgloss.Width(header) - lipgloss.Width(meta)
	if gap < 1 {
		gap = 1
	}
	headerLine := header + strings.Repeat(" ", gap) + meta

	chart := brailleChart(hist, plotW, chartH, color)
	chartLines := strings.Split(chart, "\n")
	axisLines := make([]string, chartH)
	for i := range axisLines {
		lbl := "    "
		switch i {
		case 0:
			lbl = "100%"
		case chartH / 2:
			lbl = " 50%"
		case chartH - 1:
			lbl = "  0%"
		}
		axisLines[i] = s.Muted.Render(lbl)
	}
	for i, ln := range chartLines {
		chartLines[i] = axisLines[i] + " " + ln
	}
	return strings.Join(append([]string{headerLine}, chartLines...), "\n")
}

// ---- grid view ----

// Card/focus width terms: totalW = rendered width, contentW = inner writable.
// For lipgloss + border + Padding(1,2): Width(W) produces total = W + 2,
// and interior content area = W - 4. So total - content = 6.
const cardHFrame = 6  // border(2) + horiz padding(4)
const borderOnly = 2  // horizontal border width

func (m Model) renderGrid(s styles, innerW int) string {
	if len(m.vms) == 0 {
		return ""
	}
	const gap = 2
	cardTotal := 44
	if cardTotal > innerW {
		cardTotal = innerW
	}
	cols := (innerW + gap) / (cardTotal + gap)
	if cols < 1 {
		cols = 1
	}
	cardTotal = (innerW - (cols-1)*gap) / cols
	if cardTotal < 20 {
		cardTotal = 20
	}
	contentW := cardTotal - cardHFrame
	if contentW < 14 {
		contentW = 14
	}

	cards := make([]string, 0, len(m.vms))
	for i, vm := range m.vms {
		cards = append(cards, m.renderCard(s, vm, cardTotal, contentW, i == m.selected))
	}

	var rows []string
	for i := 0; i < len(cards); i += cols {
		end := i + cols
		if end > len(cards) {
			end = len(cards)
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, spaceJoin(cards[i:end], strings.Repeat(" ", gap))...))
	}
	return strings.Join(rows, "\n\n")
}

func spaceJoin(in []string, sep string) []string {
	if len(in) == 0 {
		return in
	}
	out := make([]string, 0, 2*len(in)-1)
	for i, s := range in {
		if i > 0 {
			out = append(out, sep)
		}
		out = append(out, s)
	}
	return out
}

func (m Model) renderCard(s styles, vm lima.VM, totalW, contentW int, selected bool) string {
	th := m.theme()
	border := s.CardBorder
	if selected {
		border = s.CardBorderSel
	}

	title := lipgloss.JoinHorizontal(lipgloss.Top,
		statusDot(s, vm.Status), " ",
		s.CardTitle.Render(truncate(vm.Name, contentW-12)),
	)
	status := statusLabel(s, vm.Status)
	gap := contentW - lipgloss.Width(title) - lipgloss.Width(status)
	if gap < 1 {
		gap = 1
	}
	titleLine := title + strings.Repeat(" ", gap) + status

	meta := s.Muted.Render(truncate(fmt.Sprintf("%s · %s · %s",
		orDash(vm.Config.OS), orDash(vm.Arch), orDash(vm.VMType)), contentW))

	valueW := 10
	barW := contentW - 4 - 2 - valueW
	if barW < 6 {
		barW = 6
	}
	cpuBar := renderBar(barW, clampPct(float64(vm.CPUs)/8.0*100), th.Accent, th.Muted)
	memBar := renderBar(barW, clampPct(float64(vm.Memory)/(16*(1<<30))*100), th.Info, th.Muted)
	dskBar := renderBar(barW, clampPct(float64(vm.Disk)/(200*(1<<30))*100), th.Purple, th.Muted)

	lines := []string{
		titleLine,
		meta,
		"",
		labeledBar(s, "CPU", cpuBar, fmt.Sprintf("%d cores", vm.CPUs), valueW),
		labeledBar(s, "MEM", memBar, humanBytes(vm.Memory), valueW),
		labeledBar(s, "DSK", dskBar, humanBytes(vm.Disk), valueW),
		"",
		s.Muted.Render("SSH ") + s.Value.Render(truncate(sshTarget(vm), contentW-4)),
	}

	content := strings.Join(lines, "\n")
	return border.Width(totalW - borderOnly).Render(content)
}

func labeledBar(s styles, label, bar, value string, valueW int) string {
	return lipgloss.JoinHorizontal(lipgloss.Top,
		s.Muted.Render(fmt.Sprintf("%-4s", label)),
		bar,
		"  ",
		s.Value.Width(valueW).Render(truncate(value, valueW)),
	)
}

// ---- focus view ----

func (m Model) renderFocus(s styles, innerW, height int) string {
	vm := m.selectedVM()
	if vm == nil {
		return s.Muted.Render("no VM selected")
	}
	th := m.theme()
	totalW := innerW
	contentW := totalW - cardHFrame
	if contentW < 20 {
		contentW = 20
	}
	// keep legacy name
	width := contentW
	_ = width

	// Title
	title := lipgloss.JoinHorizontal(lipgloss.Top,
		statusDot(s, vm.Status), " ",
		s.FocusTitle.Render(vm.Name), " ",
		statusLabel(s, vm.Status),
	)
	subtitle := s.Muted.Render(fmt.Sprintf(
		"%s · %s · %s · lima %s",
		orDash(vm.Config.OS), orDash(vm.Arch), orDash(vm.VMType), orDash(vm.LimaVersion),
	))

	// Resource charts: 2-row braille area chart per metric.
	// Layout: "LBL │ [2-row chart] │ value/aux"  rendered via JoinHorizontal.
	u, haveUsage := m.usage[vm.Name]
	const labelW = 6
	const valueW = 20
	chartW := width - labelW - valueW - 2
	if chartW < 14 {
		chartW = 14
	}
	running := vm.Status == "Running"

	cpuPct := 0.0
	if haveUsage {
		cpuPct = u.CPUPct
	}
	cpuBlock := metricChart(s, "CPU", th.Accent, chartW, cpuPct,
		fmt.Sprintf("%d cores", vm.CPUs), running && haveUsage, m.cpuHistory[vm.Name])

	memPct := float64(vm.Memory) / (16 * (1 << 30)) * 100
	if haveUsage && u.MemTotal > 0 {
		memPct = u.MemPct
	}
	memLabel := humanBytes(vm.Memory) + " alloc"
	if haveUsage && u.MemTotal > 0 {
		memLabel = fmt.Sprintf("%s / %s", humanBytes(u.MemUsed), humanBytes(u.MemTotal))
	}
	memBlock := metricChart(s, "MEM", th.Info, chartW, memPct, memLabel, haveUsage, m.memHistory[vm.Name])

	dskPct := float64(vm.Disk) / (200 * (1 << 30)) * 100
	if haveUsage && u.DiskTotal > 0 {
		dskPct = u.DiskPct
	}
	dskLabel := humanBytes(vm.Disk) + " alloc"
	if haveUsage && u.DiskTotal > 0 {
		dskLabel = fmt.Sprintf("%s / %s", humanBytes(u.DiskUsed), humanBytes(u.DiskTotal))
	}
	dskBlock := metricChart(s, "DSK", th.Purple, chartW, dskPct, dskLabel, haveUsage, m.diskHistory[vm.Name])

	netHist := m.netHistory[vm.Name]
	netLabel := "idle"
	if running {
		netLabel = fmt.Sprintf("↓%s  ↑%s", humanRate(m.netRxRate[vm.Name]), humanRate(m.netTxRate[vm.Name]))
	}
	netBlock := metricChart(s, "NET", th.Success, chartW, 0, netLabel, false, scaleToPeak(netHist))

	sshInfo := lipgloss.JoinHorizontal(lipgloss.Top,
		s.Muted.Render("SSH    "), s.Value.Render(sshTarget(*vm)),
	)
	dirInfo := lipgloss.JoinHorizontal(lipgloss.Top,
		s.Muted.Render("DIR    "), s.Value.Render(compactHome(vm.Dir)),
	)
	upInfo := ""
	if haveUsage && u.Uptime != "" {
		upInfo = lipgloss.JoinHorizontal(lipgloss.Top,
			s.Muted.Render("UPTIME "), s.Value.Render(u.Uptime))
	}

	infoBlock := strings.Join(nonEmpty([]string{
		title, subtitle, "",
		cpuBlock, "",
		memBlock, "",
		dskBlock, "",
		netBlock, "",
		sshInfo, dirInfo, upInfo,
	}), "\n")

	header := s.FocusCard.Width(totalW - borderOnly).Render(infoBlock)

	remaining := height - lipgloss.Height(header) - 2
	procPanel := m.renderProcessPanel(s, totalW, contentW, remaining, vm)

	return lipgloss.JoinVertical(lipgloss.Left, header, "", procPanel)
}

// metricChart renders a 2-row braille area chart for a single metric, flanked
// by a name + live % on the left and an auxiliary label on the right. When
// `live` is false the percent is shown as a dash.
func metricChart(s styles, label string, color lipgloss.Color, chartW int, pct float64, extra string, live bool, history []float64) string {
	const rows = 2
	chart := brailleChart(history, chartW, rows, color)
	chartLines := strings.Split(chart, "\n")
	for len(chartLines) < rows {
		chartLines = append(chartLines, strings.Repeat(" ", chartW))
	}

	name := s.Muted.Render(fmt.Sprintf("%-4s", label))
	pctStr := s.Muted.Render("  — ")
	if live {
		pctStr = s.Value.Render(fmt.Sprintf("%4.0f%%", pct))
	}
	left := lipgloss.JoinVertical(lipgloss.Left, name, pctStr)

	right := lipgloss.JoinVertical(lipgloss.Left,
		s.Value.Render(extra),
		s.Muted.Render(""),
	)

	return lipgloss.JoinHorizontal(lipgloss.Top,
		left, " ",
		lipgloss.JoinVertical(lipgloss.Left, chartLines...),
		" ", right,
	)
}

func (m Model) renderProcessPanel(s styles, totalW, contentW, height int, vm *lima.VM) string {
	lgw := totalW - borderOnly
	if vm.Status != "Running" {
		return s.ProcessCard.Width(lgw).Render(
			s.Muted.Render(fmt.Sprintf("VM is %s — start it to see processes.", strings.ToLower(vm.Status))),
		)
	}

	if m.procsErr != nil && len(m.procs) == 0 {
		return s.ProcessCard.Width(lgw).Render(
			s.Error.Render("could not list processes: " + m.procsErr.Error()),
		)
	}
	if len(m.procs) == 0 {
		return s.ProcessCard.Width(lgw).Render(s.Muted.Render("collecting processes…"))
	}

	procs := append([]lima.Process(nil), m.procs...)
	sort.SliceStable(procs, func(i, j int) bool {
		return parseFloat(procs[i].CPU) > parseFloat(procs[j].CPU)
	})

	maxRows := height - 3
	if maxRows > 20 {
		maxRows = 20
	}
	if maxRows < 3 {
		maxRows = 3
	}
	if maxRows > len(procs) {
		maxRows = len(procs)
	}

	cols := []struct {
		name  string
		width int
	}{
		{"PID", 7},
		{"USER", 12},
		{"CPU%", 7},
		{"MEM%", 7},
		{"COMMAND", contentW - 7 - 12 - 7 - 7 - 4},
	}
	if cols[4].width < 10 {
		cols[4].width = 10
	}

	var hdr strings.Builder
	for _, c := range cols {
		hdr.WriteString(s.TableHeader.Width(c.width).Render(c.name))
		hdr.WriteString(" ")
	}
	rows := []string{
		s.ProcessTitle.Render(fmt.Sprintf("processes · top %d by cpu", maxRows)),
		"",
		hdr.String(),
		s.Muted.Render(strings.Repeat("─", contentW)),
	}
	for i := 0; i < maxRows; i++ {
		p := procs[i]
		row := lipgloss.JoinHorizontal(lipgloss.Top,
			s.Value.Width(cols[0].width).Render(truncate(p.PID, cols[0].width)), " ",
			s.Muted.Width(cols[1].width).Render(truncate(p.User, cols[1].width)), " ",
			s.Accent.Width(cols[2].width).Render(truncate(p.CPU, cols[2].width)), " ",
			s.Info.Width(cols[3].width).Render(truncate(p.Mem, cols[3].width)), " ",
			s.Value.Width(cols[4].width).Render(truncate(p.Cmd, cols[4].width)),
		)
		rows = append(rows, row)
	}
	return s.ProcessCard.Width(lgw).Render(strings.Join(rows, "\n"))
}

// ---- bar / helpers ----

// brailleChart renders `samples` (0..100 each) as a right-aligned area chart
// `width` cells wide and `height` rows tall, using unicode braille so each
// cell carries 2 columns × 4 rows of dot resolution.
func brailleChart(samples []float64, width, height int, fg lipgloss.Color) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	dotsW := width * 2
	dotsH := height * 4

	start := 0
	if len(samples) > dotsW {
		start = len(samples) - dotsW
	}
	vis := samples[start:]
	offset := dotsW - len(vis) // left blank dot-columns when history < width

	bits := make([][]int, height)
	for i := range bits {
		bits[i] = make([]int, width)
	}
	// [subCol][subRow] → bit mask, subRow 0=top, 3=bottom.
	dotBit := [2][4]int{
		{0x01, 0x02, 0x04, 0x40},
		{0x08, 0x10, 0x20, 0x80},
	}
	for i, v := range vis {
		dotCol := offset + i
		cellCol := dotCol / 2
		subCol := dotCol % 2
		if cellCol >= width {
			break
		}
		v = clampPct(v)
		topDot := int(v / 100 * float64(dotsH))
		if topDot > dotsH {
			topDot = dotsH
		}
		// Any non-zero sample gets at least one dot so a running baseline
		// is visible even when utilization is well below one quantum
		// (important at height=1, where one quantum = 25%).
		if topDot == 0 && v > 0 {
			topDot = 1
		}
		// Fill from the bottom (r=0) up to the sample's height.
		for r := 0; r < topDot; r++ {
			cellRow := height - 1 - r/4
			if cellRow < 0 {
				break
			}
			subRow := 3 - r%4
			bits[cellRow][cellCol] |= dotBit[subCol][subRow]
		}
	}

	style := lipgloss.NewStyle().Foreground(fg)
	lines := make([]string, height)
	for r, row := range bits {
		var b strings.Builder
		for _, x := range row {
			b.WriteRune(rune(0x2800 + x))
		}
		lines[r] = style.Render(b.String())
	}
	return strings.Join(lines, "\n")
}

// sparkCell renders a 1-row braille area chart plus "pct%" for a metric
// column, or a dash for VMs with no live history yet.
func (m Model) sparkCell(vm lima.VM, hist []float64, color lipgloss.Color, th theme.Theme, width int) string {
	if vm.Status != "Running" {
		return lipgloss.NewStyle().Foreground(th.Muted).Render("—")
	}
	if len(hist) == 0 {
		return lipgloss.NewStyle().Foreground(th.Muted).Render("…")
	}
	sparkW := width - 5 // reserve " 42%" (5 chars) on the right
	if sparkW < 4 {
		sparkW = 4
	}
	spark := brailleChart(hist, sparkW, 1, color)
	pct := hist[len(hist)-1]
	pctStr := lipgloss.NewStyle().Foreground(th.Foreground).Render(fmt.Sprintf("%4.0f%%", pct))
	return spark + " " + pctStr
}

// netCell renders the NET% sparkline cell: a 1-row braille chart of the
// combined rx+tx rate (scaled to peak) plus a compact current rate.
func (m Model) netCell(vm lima.VM, th theme.Theme, width int) string {
	if vm.Status != "Running" {
		return lipgloss.NewStyle().Foreground(th.Muted).Render("—")
	}
	hist := m.netHistory[vm.Name]
	if len(hist) == 0 {
		return lipgloss.NewStyle().Foreground(th.Muted).Render("…")
	}
	rate := m.netRxRate[vm.Name] + m.netTxRate[vm.Name]
	rateStr := compactRate(rate)
	sparkW := width - lipgloss.Width(rateStr) - 1
	if sparkW < 4 {
		sparkW = 4
	}
	spark := brailleChart(scaleToPeak(hist), sparkW, 1, th.Success)
	return spark + " " + lipgloss.NewStyle().Foreground(th.Foreground).Render(rateStr)
}

// compactRate renders bytes/sec in a minimal 5-char form: "1.2M", " 350K", etc.
func compactRate(bps float64) string {
	if bps <= 0 {
		return "   0 "
	}
	units := []struct {
		div   float64
		label byte
	}{
		{1 << 30, 'G'},
		{1 << 20, 'M'},
		{1 << 10, 'K'},
	}
	for _, u := range units {
		if bps >= u.div {
			return fmt.Sprintf("%4.1f%c", bps/u.div, u.label)
		}
	}
	return fmt.Sprintf("%4.0fB", bps)
}

func renderBar(width int, pct float64, fg, bg lipgloss.Color) string {
	if width < 1 {
		return ""
	}
	filled := int(float64(width) * pct / 100)
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	fill := lipgloss.NewStyle().Foreground(fg).Render(strings.Repeat("█", filled))
	rest := lipgloss.NewStyle().Foreground(bg).Render(strings.Repeat("░", width-filled))
	return fill + rest
}

func statusDot(s styles, status string) string {
	c := s.Muted
	switch status {
	case "Running":
		c = s.Success
	case "Stopped", "Broken":
		c = s.Error
	case "Starting", "Stopping":
		c = s.Warning
	}
	return c.Render("●")
}

func statusLabel(s styles, status string) string {
	if status == "" {
		status = "Unknown"
	}
	c := s.Muted
	switch status {
	case "Running":
		c = s.Success
	case "Stopped", "Broken":
		c = s.Error
	case "Starting", "Stopping":
		c = s.Warning
	}
	return c.Render(status)
}

func sshTarget(vm lima.VM) string {
	if vm.SSHLocalPort == 0 {
		return "—"
	}
	host := vm.SSHAddress
	if host == "" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("%s:%d", host, vm.SSHLocalPort)
}

// humanRate formats a bytes/second value with an auto-selected unit.
func humanRate(bps float64) string {
	if bps <= 0 {
		return "0 B/s"
	}
	const unit = 1024.0
	if bps < unit {
		return fmt.Sprintf("%.0f B/s", bps)
	}
	div, exp := unit, 0
	for n := bps / unit; n >= unit && exp < 5; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB/s", bps/div, "KMGTPE"[exp])
}

// scaleToPeak normalizes samples to 0..100 relative to the max observed.
// Zero-peak (no data yet) returns zeros so an empty chart renders flat.
func scaleToPeak(in []float64) []float64 {
	peak := 0.0
	for _, v := range in {
		if v > peak {
			peak = v
		}
	}
	if peak <= 0 {
		return in
	}
	out := make([]float64, len(in))
	for i, v := range in {
		out[i] = v / peak * 100
	}
	return out
}

func humanBytes(b int64) string {
	if b <= 0 {
		return "—"
	}
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit && exp < 5; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func intOrDash(n int) string {
	if n == 0 {
		return "—"
	}
	return fmt.Sprintf("%d", n)
}

func bytesOrDash(b int64) string {
	if b <= 0 {
		return "—"
	}
	return humanBytes(b)
}

// diskUsage shows live "used / total" from the guest's df output when
// available; otherwise falls back to the allocated disk size.
func diskUsage(vm lima.VM, u lima.Usage) string {
	if vm.Status == "Running" && u.DiskTotal > 0 {
		return fmt.Sprintf("%s / %s", humanBytes(u.DiskUsed), humanBytes(u.DiskTotal))
	}
	return bytesOrDash(vm.Disk)
}

func orDash(v string) string {
	if v == "" {
		return "—"
	}
	return v
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r)+"…") > w {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

func clampPct(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func nonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if strings.TrimSpace(s) != "" || s == "" {
			out = append(out, s)
		}
	}
	return out
}

func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

func compactHome(p string) string {
	if p == "" {
		return "—"
	}
	home := homeDir()
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
