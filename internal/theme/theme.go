package theme

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/pelletier/go-toml/v2"
)

// Theme is a small palette derived from a terminal colorscheme.
type Theme struct {
	Name       string
	Background lipgloss.Color
	Foreground lipgloss.Color
	Muted      lipgloss.Color
	Accent     lipgloss.Color
	Success    lipgloss.Color
	Warning    lipgloss.Color
	Error      lipgloss.Color
	Info       lipgloss.Color
	Purple     lipgloss.Color
}

type alacrittyTOML struct {
	Colors struct {
		Primary struct {
			Background string `toml:"background"`
			Foreground string `toml:"foreground"`
		} `toml:"primary"`
		Normal struct {
			Black   string `toml:"black"`
			Red     string `toml:"red"`
			Green   string `toml:"green"`
			Yellow  string `toml:"yellow"`
			Blue    string `toml:"blue"`
			Magenta string `toml:"magenta"`
			Cyan    string `toml:"cyan"`
			White   string `toml:"white"`
		} `toml:"normal"`
		Bright struct {
			Black   string `toml:"black"`
			Red     string `toml:"red"`
			Green   string `toml:"green"`
			Yellow  string `toml:"yellow"`
			Blue    string `toml:"blue"`
			Magenta string `toml:"magenta"`
			Cyan    string `toml:"cyan"`
			White   string `toml:"white"`
		} `toml:"bright"`
	} `toml:"colors"`
}

func Nord() Theme {
	return Theme{
		Name:       "nord",
		Background: "#2e3440",
		Foreground: "#d8dee9",
		Muted:      "#4c566a",
		Accent:     "#88c0d0",
		Success:    "#a3be8c",
		Warning:    "#ebcb8b",
		Error:      "#bf616a",
		Info:       "#81a1c1",
		Purple:     "#b48ead",
	}
}

func TokyoNight() Theme {
	return Theme{
		Name:       "tokyonight",
		Background: "#1a1b26",
		Foreground: "#c0caf5",
		Muted:      "#565f89",
		Accent:     "#7dcfff",
		Success:    "#9ece6a",
		Warning:    "#e0af68",
		Error:      "#f7768e",
		Info:       "#7aa2f7",
		Purple:     "#bb9af7",
	}
}

func Gruvbox() Theme {
	return Theme{
		Name:       "gruvbox",
		Background: "#282828",
		Foreground: "#ebdbb2",
		Muted:      "#928374",
		Accent:     "#83a598",
		Success:    "#b8bb26",
		Warning:    "#fabd2f",
		Error:      "#fb4934",
		Info:       "#83a598",
		Purple:     "#d3869b",
	}
}

func CatppuccinMocha() Theme {
	return Theme{
		Name:       "catppuccin",
		Background: "#1e1e2e",
		Foreground: "#cdd6f4",
		Muted:      "#6c7086",
		Accent:     "#94e2d5",
		Success:    "#a6e3a1",
		Warning:    "#f9e2af",
		Error:      "#f38ba8",
		Info:       "#89b4fa",
		Purple:     "#cba6f7",
	}
}

// Omarchy reads the current omarchy alacritty colors and maps them to a Theme.
func Omarchy() (Theme, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Theme{}, err
	}
	path := filepath.Join(home, ".config", "omarchy", "current", "theme", "alacritty.toml")
	b, err := os.ReadFile(path)
	if err != nil {
		return Theme{}, err
	}
	var a alacrittyTOML
	if err := toml.Unmarshal(b, &a); err != nil {
		return Theme{}, err
	}
	if a.Colors.Primary.Background == "" || a.Colors.Normal.Green == "" {
		return Theme{}, fmt.Errorf("omarchy alacritty.toml missing expected colors")
	}
	muted := a.Colors.Bright.Black
	if muted == "" {
		muted = a.Colors.Normal.Black
	}
	return Theme{
		Name:       "omarchy",
		Background: lipgloss.Color(a.Colors.Primary.Background),
		Foreground: lipgloss.Color(a.Colors.Primary.Foreground),
		Muted:      lipgloss.Color(muted),
		Accent:     lipgloss.Color(a.Colors.Normal.Cyan),
		Success:    lipgloss.Color(a.Colors.Normal.Green),
		Warning:    lipgloss.Color(a.Colors.Normal.Yellow),
		Error:      lipgloss.Color(a.Colors.Normal.Red),
		Info:       lipgloss.Color(a.Colors.Normal.Blue),
		Purple:     lipgloss.Color(a.Colors.Normal.Magenta),
	}, nil
}

// All returns the theme cycle. Omarchy is first when available.
func All() []Theme {
	out := []Theme{}
	if t, err := Omarchy(); err == nil {
		out = append(out, t)
	}
	out = append(out, Nord(), TokyoNight(), CatppuccinMocha(), Gruvbox())
	return out
}
