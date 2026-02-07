package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/burritocatai/civcat/internal/api"
	"github.com/burritocatai/civcat/internal/config"
	"github.com/burritocatai/civcat/internal/tracker"
	"github.com/burritocatai/civcat/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "civcat",
	Short: "Civitai model manager for ComfyUI",
	Long:  "civcat is a TUI application for browsing, downloading, and managing Civitai models for use with ComfyUI.",
	RunE:  runApp,
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configure civcat settings",
	RunE:  runConfig,
}

func Execute() error {
	rootCmd.AddCommand(configCmd)
	return rootCmd.Execute()
}

func runApp(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// First-run setup if not configured.
	if !cfg.IsConfigured() {
		fmt.Println("Welcome to civcat! Let's set up your configuration.")
		fmt.Println()
		if err := interactiveConfig(cfg); err != nil {
			return err
		}
	}

	client := api.NewClient(cfg.GetAPIKey())

	trk, err := tracker.New()
	if err != nil {
		return fmt.Errorf("initializing tracker: %w", err)
	}

	app := tui.NewApp(cfg, client, trk)
	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}

	return nil
}

func runConfig(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if err := interactiveConfig(cfg); err != nil {
		return err
	}

	fmt.Println("Configuration saved!")
	return nil
}

func interactiveConfig(cfg *config.Config) error {
	reader := bufio.NewReader(os.Stdin)

	// ComfyUI path
	current := cfg.ComfyUIPath
	if current == "" {
		current = "(not set)"
	}
	fmt.Printf("ComfyUI installation path [%s]: ", current)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line != "" {
		// Validate directory exists.
		info, err := os.Stat(line)
		if err != nil || !info.IsDir() {
			fmt.Printf("Warning: %s does not exist or is not a directory. Save anyway? [y/N]: ", line)
			confirm, _ := reader.ReadString('\n')
			confirm = strings.TrimSpace(strings.ToLower(confirm))
			if confirm != "y" && confirm != "yes" {
				return fmt.Errorf("configuration cancelled")
			}
		}
		cfg.ComfyUIPath = line
	}

	// API key
	currentKey := cfg.APIKey
	envKey := os.Getenv("CIVITAI_API_KEY")
	if envKey != "" {
		fmt.Println("API key is set via CIVITAI_API_KEY environment variable.")
	} else {
		display := "(not set)"
		if currentKey != "" {
			display = currentKey[:4] + "..." + currentKey[len(currentKey)-4:]
		}
		fmt.Printf("Civitai API key [%s]: ", display)
		line, _ = reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" {
			cfg.APIKey = line
		}
	}

	return cfg.Save()
}
