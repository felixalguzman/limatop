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

// ---- model ----

type Model struct {
	themes   []theme.Theme
	themeIdx int
	view     View

	vms      []lima.VM
	selected int

	procs    []lima.Process
	procsErr error

	usage    map[string]lima.Usage
	usageErr map[string]error

	width  int
	height int

	loading bool
	err     error
}

func New() Model {
	return Model{
		themes:   theme.All(),
		view:     ViewTable,
		loading:  true,
		usage:    map[string]lima.Usage{},
		usageErr: map[string]error{},
	}
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

// ---- update ----

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		return m.handleKey(msg)

	case vmsMsg:
		m.loading = false
		m.err = msg.err
		m.vms = msg.vms
		if m.selected >= len(m.vms) {
			m.selected = len(m.vms) - 1
		}
		if m.selected < 0 {
			m.selected = 0
		}
		if m.view == ViewFocus {
			if vm := m.selectedVM(); vm != nil && vm.Status == "Running" {
				return m, tea.Batch(fetchProcs(vm.Name), fetchUsage(vm.Name))
			}
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
		}

	case tickMsg:
		cmds := []tea.Cmd{fetchVMs(), tick()}
		if m.view == ViewFocus {
			if vm := m.selectedVM(); vm != nil && vm.Status == "Running" {
				cmds = append(cmds, fetchProcs(vm.Name), fetchUsage(vm.Name))
			}
		}
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	default:
		switch m.view {
		case ViewTable:
			body = m.renderTable(s, innerW)
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
	right := s.HeaderMeta.Render(fmt.Sprintf(" %s ", time.Now().Format("15:04:05")))

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	bar := left + strings.Repeat(" ", gap) + right
	return s.HeaderBar.Width(m.width).Render(bar)
}

func (m Model) renderFooter(s styles) string {
	keys := []string{
		"j/k move", "enter focus", "v view", "1/2/3 jump", "r refresh", "t theme", "q quit",
	}
	var parts []string
	for _, k := range keys {
		parts = append(parts, s.Key.Render(k))
	}
	hint := strings.Join(parts, s.Muted.Render(" · "))
	return s.FooterBar.Width(m.width).Render(" " + hint + " ")
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
	cols := []struct {
		name  string
		width int
	}{
		{"", 2},
		{"NAME", 18},
		{"STATUS", 10},
		{"CPU", 5},
		{"MEM", 10},
		{"DISK", 10},
		{"OS", 7},
		{"ARCH", 9},
		{"TYPE", 6},
		{"SSH", 22},
	}

	total := 0
	for _, c := range cols {
		total += c.width + 1
	}
	if total > innerW {
		overflow := total - innerW
		cols[len(cols)-1].width -= overflow
		if cols[len(cols)-1].width < 3 {
			cols[len(cols)-1].width = 3
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

	for i, vm := range m.vms {
		selected := i == m.selected
		cells := []string{
			statusDot(s, vm.Status),
			vm.Name,
			statusLabel(s, vm.Status),
			intOrDash(vm.CPUs),
			bytesOrDash(vm.Memory),
			bytesOrDash(vm.Disk),
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
			if idx == 0 {
				row.WriteString(lipgloss.NewStyle().Width(c.width).Render(cell))
			} else if idx == 2 {
				// status label keeps its own color
				row.WriteString(lipgloss.NewStyle().Width(c.width).Render(truncate(cell, c.width)))
			} else {
				row.WriteString(rowStyle.Width(c.width).Render(truncate(cell, c.width)))
			}
			row.WriteString(" ")
		}
		rows = append(rows, row.String())
	}

	return strings.Join(rows, "\n")
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

	// Resource block
	barW := width - 36
	if barW < 10 {
		barW = 10
	}
	u, haveUsage := m.usage[vm.Name]
	cpuLine := focusResource(s, "CPU", barW, th.Accent, th.Muted,
		ternary(haveUsage, u.CPUPct, 0),
		fmt.Sprintf("%d cores", vm.CPUs),
		haveUsage && vm.Status == "Running",
	)
	memPct := float64(vm.Memory) / (16 * (1 << 30)) * 100
	if haveUsage && u.MemTotal > 0 {
		memPct = u.MemPct
	}
	memLabel := humanBytes(vm.Memory) + " alloc"
	if haveUsage && u.MemTotal > 0 {
		memLabel = fmt.Sprintf("%s / %s", humanBytes(u.MemUsed), humanBytes(u.MemTotal))
	}
	memLine := focusResource(s, "MEM", barW, th.Info, th.Muted, memPct, memLabel, haveUsage)

	dskPct := float64(vm.Disk) / (200 * (1 << 30)) * 100
	if haveUsage && u.DiskTotal > 0 {
		dskPct = u.DiskPct
	}
	dskLabel := humanBytes(vm.Disk) + " alloc"
	if haveUsage && u.DiskTotal > 0 {
		dskLabel = fmt.Sprintf("%s / %s", humanBytes(u.DiskUsed), humanBytes(u.DiskTotal))
	}
	dskLine := focusResource(s, "DSK", barW, th.Purple, th.Muted, dskPct, dskLabel, haveUsage)

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
		cpuLine, memLine, dskLine, "",
		sshInfo, dirInfo, upInfo,
	}), "\n")

	header := s.FocusCard.Width(totalW - borderOnly).Render(infoBlock)

	procPanel := m.renderProcessPanel(s, totalW, contentW, height-lipgloss.Height(header)-2, vm)

	return lipgloss.JoinVertical(lipgloss.Left, header, "", procPanel)
}

func focusResource(s styles, label string, barW int, fg, bg lipgloss.Color, pct float64, extra string, live bool) string {
	bar := renderBar(barW, clampPct(pct), fg, bg)
	pctStr := "  · "
	if live {
		pctStr = s.Value.Render(fmt.Sprintf("%5.1f%% ", pct))
	} else {
		pctStr = s.Muted.Render("  —    ")
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		s.Muted.Render(fmt.Sprintf("%-4s", label)),
		bar, " ",
		pctStr, " ",
		s.Value.Render(extra),
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

func ternary(cond bool, a, b float64) float64 {
	if cond {
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
