package tui

import (
	"os"

	"github.com/charmbracelet/lipgloss"

	"github.com/felixalguzman/limatop/internal/theme"
)

type styles struct {
	Base  lipgloss.Style
	Muted lipgloss.Style
	Value lipgloss.Style

	Accent  lipgloss.Style
	Info    lipgloss.Style
	Success lipgloss.Style
	Warning lipgloss.Style
	Error   lipgloss.Style

	HeaderBar  lipgloss.Style
	HeaderMeta lipgloss.Style
	Title      lipgloss.Style

	FooterBar lipgloss.Style
	Key       lipgloss.Style
	KeyCap    lipgloss.Style

	TableHeader    lipgloss.Style
	Row            lipgloss.Style
	RowSelected    lipgloss.Style
	RowSelectedBar lipgloss.Style

	CardBorder    lipgloss.Style
	CardBorderSel lipgloss.Style
	CardTitle     lipgloss.Style

	FocusCard    lipgloss.Style
	FocusTitle   lipgloss.Style
	ProcessCard  lipgloss.Style
	ProcessTitle lipgloss.Style
}

func (m Model) styles() styles {
	t := m.theme()
	base := lipgloss.NewStyle().Foreground(t.Foreground)

	rounded := lipgloss.RoundedBorder()

	return styles{
		Base:    base,
		Muted:   lipgloss.NewStyle().Foreground(t.Muted),
		Value:   lipgloss.NewStyle().Foreground(t.Foreground),
		Accent:  lipgloss.NewStyle().Foreground(t.Accent),
		Info:    lipgloss.NewStyle().Foreground(t.Info),
		Success: lipgloss.NewStyle().Foreground(t.Success),
		Warning: lipgloss.NewStyle().Foreground(t.Warning),
		Error:   lipgloss.NewStyle().Foreground(t.Error),

		HeaderBar: lipgloss.NewStyle().
			Foreground(t.Foreground).
			Background(t.Background),
		HeaderMeta: lipgloss.NewStyle().
			Foreground(t.Muted),
		Title: lipgloss.NewStyle().
			Foreground(t.Background).
			Background(t.Accent).
			Bold(true),

		FooterBar: lipgloss.NewStyle().
			Foreground(t.Muted),
		Key: lipgloss.NewStyle().Foreground(t.Foreground),
		KeyCap: lipgloss.NewStyle().
			Foreground(t.Accent).
			Bold(true),

		TableHeader: lipgloss.NewStyle().
			Foreground(t.Muted).
			Bold(true),
		Row: lipgloss.NewStyle().Foreground(t.Foreground),
		RowSelected: lipgloss.NewStyle().
			Foreground(t.Foreground).
			Bold(true),
		RowSelectedBar: lipgloss.NewStyle().Foreground(t.Accent).Bold(true),

		CardBorder: lipgloss.NewStyle().
			Border(rounded).
			BorderForeground(t.Muted).
			Padding(1, 2),
		CardBorderSel: lipgloss.NewStyle().
			Border(rounded).
			BorderForeground(t.Accent).
			Padding(1, 2),
		CardTitle: lipgloss.NewStyle().
			Foreground(t.Foreground).
			Bold(true),

		FocusCard: lipgloss.NewStyle().
			Border(rounded).
			BorderForeground(t.Accent).
			Padding(1, 2),
		FocusTitle: lipgloss.NewStyle().
			Foreground(t.Accent).
			Bold(true),
		ProcessCard: lipgloss.NewStyle().
			Border(rounded).
			BorderForeground(t.Muted).
			Padding(1, 2),
		ProcessTitle: lipgloss.NewStyle().
			Foreground(t.Purple).
			Bold(true),
	}
}

// styles_test helper.
var _ = theme.Nord

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}
