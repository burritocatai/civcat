package tui

import "github.com/charmbracelet/lipgloss"

var (
	accentColor   = lipgloss.Color("#7C3AED")
	successColor  = lipgloss.Color("#10B981")
	warningColor  = lipgloss.Color("#F59E0B")
	errorColor    = lipgloss.Color("#EF4444")
	mutedColor    = lipgloss.Color("#6B7280")
	whiteColor    = lipgloss.Color("#F9FAFB")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(whiteColor).
			Background(accentColor).
			Padding(0, 1)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true)

	selectedStyle = lipgloss.NewStyle().
			Foreground(whiteColor).
			Background(accentColor).
			Padding(0, 1)

	normalItemStyle = lipgloss.NewStyle().
			Padding(0, 1)

	mutedStyle = lipgloss.NewStyle().
			Foreground(mutedColor)

	successStyle = lipgloss.NewStyle().
			Foreground(successColor)

	warningStyle = lipgloss.NewStyle().
			Foreground(warningColor)

	errorStyle = lipgloss.NewStyle().
			Foreground(errorColor)

	helpStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Padding(1, 0)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(whiteColor).
			Background(lipgloss.Color("#374151")).
			Padding(0, 1)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accentColor).
			Padding(0, 1)

	inputStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(accentColor).
			Padding(0, 1)
)
