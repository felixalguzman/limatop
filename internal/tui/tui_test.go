package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/felixalguzman/limatop/internal/lima"
)

func seedModel() Model {
	m := New()
	m.width = 140
	m.height = 40
	m.loading = false
	m.vms = []lima.VM{
		{Name: "test", Status: "Running", Arch: "x86_64", VMType: "qemu",
			CPUs: 4, Memory: 4 << 30, Disk: 100 << 30,
			SSHLocalPort: 36965, SSHAddress: "127.0.0.1", LimaVersion: "2.1.1",
			Dir: "/home/u/.lima/test",
		},
		{Name: "stopped-vm", Status: "Stopped", Arch: "aarch64", VMType: "qemu",
			CPUs: 2, Memory: 2 << 30, Disk: 50 << 30},
	}
	m.vms[0].Config.OS = "Linux"
	m.vms[1].Config.OS = "Linux"
	return m
}

func TestRenderAllViews(t *testing.T) {
	m := seedModel()
	for _, v := range []View{ViewTable, ViewGrid, ViewFocus} {
		m.view = v
		out := m.View()
		if out == "" {
			t.Fatalf("view %d rendered empty string", v)
		}
		if !strings.Contains(out, "test") {
			t.Fatalf("view %d missing vm name, got:\n%s", v, out)
		}
		if !strings.Contains(out, "limatop") {
			t.Fatalf("view %d missing title, got:\n%s", v, out)
		}
	}
}

func TestThemeCycle(t *testing.T) {
	m := seedModel()
	start := m.theme().Name
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if m2.(Model).theme().Name == start && len(m.themes) > 1 {
		t.Fatalf("theme did not cycle: still %q", start)
	}
}

func TestSelectionBounds(t *testing.T) {
	m := seedModel()
	// j moves down
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m2.(Model).selected != 1 {
		t.Fatalf("expected selected=1 after j, got %d", m2.(Model).selected)
	}
	// j again must not overflow
	m3, _ := m2.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m3.(Model).selected != 1 {
		t.Fatalf("expected selected stays at 1, got %d", m3.(Model).selected)
	}
	// k moves up
	m4, _ := m3.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m4.(Model).selected != 0 {
		t.Fatalf("expected selected=0, got %d", m4.(Model).selected)
	}
}

func TestSparklinePopulates(t *testing.T) {
	m := seedModel()
	m.cpuHistory["test"] = []float64{5, 25, 55, 95, 70, 30}
	out := m.View()
	// Expect at least one block glyph from the sparkline palette.
	if !strings.ContainsAny(out, "▁▂▃▄▅▆▇█") {
		t.Fatalf("expected sparkline block chars in rendered table, got:\n%s", out)
	}
}

func TestFocusChartHasBraille(t *testing.T) {
	m := seedModel()
	m.view = ViewFocus
	// A non-trivial history so the chart fills some dots.
	var hist []float64
	for i := 0; i < 40; i++ {
		hist = append(hist, float64(20+(i*3)%60))
	}
	m.cpuHistory["test"] = hist
	out := m.View()
	// Expect at least one "filled" braille char (not the all-blank U+2800).
	filled := false
	for _, r := range out {
		if r > 0x2800 && r <= 0x28FF {
			filled = true
			break
		}
	}
	if !filled {
		t.Fatalf("expected populated braille chart in focus view, got:\n%s", out)
	}
	if !strings.Contains(out, "cpu history") {
		t.Fatalf("expected chart panel title, got:\n%s", out)
	}
}

func TestDeleteRequiresConfirmation(t *testing.T) {
	m := seedModel()
	// pressing d opens the confirm modal, does not dispatch delete
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	mm := m2.(Model)
	if mm.confirmDelete != "test" {
		t.Fatalf("expected confirmDelete=test, got %q", mm.confirmDelete)
	}
	if cmd != nil {
		t.Fatalf("d should not dispatch a command until confirmed")
	}
	if !strings.Contains(mm.View(), "Delete VM?") {
		t.Fatalf("confirm modal not rendered")
	}
	// n cancels
	m3, _ := mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if m3.(Model).confirmDelete != "" {
		t.Fatalf("n should cancel confirmation")
	}
}

func TestEnterGoesToFocus(t *testing.T) {
	m := seedModel()
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m2.(Model).view != ViewFocus {
		t.Fatalf("enter did not switch to focus")
	}
}

// Dump the rendered frame of every view to testdata for visual sanity check.
// Run with: go test -run Dump -v ./internal/tui
func TestDumpFrames(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	m := seedModel()
	for name, v := range map[string]View{"table": ViewTable, "grid": ViewGrid, "focus": ViewFocus} {
		m.view = v
		t.Logf("\n=== %s ===\n%s", name, m.View())
	}
}
