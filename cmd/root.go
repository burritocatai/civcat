package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/burritocatai/civcat/internal/api"
	"github.com/burritocatai/civcat/internal/config"
	"github.com/burritocatai/civcat/internal/downloader"
	"github.com/burritocatai/civcat/internal/hfapi"
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

var exportCmd = &cobra.Command{
	Use:   "export <file>",
	Short: "Export installed models list to a JSON file",
	Args:  cobra.ExactArgs(1),
	RunE:  runExport,
}

var importCmd = &cobra.Command{
	Use:   "import <file>",
	Short: "Import models from an export file and download missing ones",
	Args:  cobra.ExactArgs(1),
	RunE:  runImport,
}

func Execute() error {
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(exportCmd)
	rootCmd.AddCommand(importCmd)
	return rootCmd.Execute()
}

func runApp(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	client := api.NewClient(cfg.GetAPIKey())
	hfClient := hfapi.NewClient(cfg.GetHFToken())

	trk, err := tracker.New()
	if err != nil {
		return fmt.Errorf("initializing tracker: %w", err)
	}

	app := tui.NewApp(cfg, client, hfClient, trk)
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

func runExport(cmd *cobra.Command, args []string) error {
	trk, err := tracker.New()
	if err != nil {
		return fmt.Errorf("initializing tracker: %w", err)
	}

	path := args[0]
	if err := trk.Export(path); err != nil {
		return fmt.Errorf("exporting models: %w", err)
	}

	models := trk.GetAll()
	fmt.Printf("Exported %d model(s) to %s\n", len(models), path)
	return nil
}

func runImport(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if cfg.ComfyUIPath == "" {
		return fmt.Errorf("ComfyUI path not configured. Run 'civcat config' first")
	}

	client := api.NewClient(cfg.GetAPIKey())

	trk, err := tracker.New()
	if err != nil {
		return fmt.Errorf("initializing tracker: %w", err)
	}

	data, err := tracker.LoadExport(args[0])
	if err != nil {
		return err
	}

	// Determine which models are missing.
	var missing []tracker.ExportModel
	for _, em := range data.Models {
		if !trk.IsInstalled(em.ModelID) {
			missing = append(missing, em)
		}
	}

	fmt.Printf("Found %d model(s) in export, %d already installed, %d to download\n",
		len(data.Models), len(data.Models)-len(missing), len(missing))

	if len(missing) == 0 {
		fmt.Println("Nothing to do — all models are already installed.")
		return nil
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Download %d missing model(s)? [y/N]: ", len(missing))
	confirm, _ := reader.ReadString('\n')
	confirm = strings.TrimSpace(strings.ToLower(confirm))
	if confirm != "y" && confirm != "yes" {
		fmt.Println("Import cancelled.")
		return nil
	}

	for i, em := range missing {
		fmt.Printf("\n[%d/%d] Fetching %s (model %d, version %d)...\n",
			i+1, len(missing), em.ModelName, em.ModelID, em.VersionID)

		model, err := client.GetModel(em.ModelID)
		if err != nil {
			fmt.Printf("  ERROR fetching model: %v — skipping\n", err)
			continue
		}

		// Find the matching version.
		var version *api.ModelVersion
		for idx := range model.Versions {
			if model.Versions[idx].ID == em.VersionID {
				version = &model.Versions[idx]
				break
			}
		}
		if version == nil {
			// Version no longer available — try the latest version instead.
			if len(model.Versions) > 0 {
				version = &model.Versions[0]
				fmt.Printf("  Version %d not found, using latest: %s (v%d)\n",
					em.VersionID, version.Name, version.ID)
			} else {
				fmt.Printf("  ERROR: no versions available — skipping\n")
				continue
			}
		}

		if version.IsEarlyAccess() {
			fmt.Printf("  Skipping — version is in early access (%d days remaining)\n",
				version.EarlyAccessDaysLeft())
			continue
		}

		// Download with progress.
		progressCh := make(chan downloader.Progress, 64)
		go func() {
			for p := range progressCh {
				if p.Done || p.Err != nil {
					return
				}
				if p.TotalBytes > 0 {
					pct := float64(p.BytesDownloaded) / float64(p.TotalBytes) * 100
					fmt.Printf("\r  Downloading... %s / %s (%.1f%%)",
						formatBytesCmd(p.BytesDownloaded),
						formatBytesCmd(p.TotalBytes),
						pct)
				} else {
					fmt.Printf("\r  Downloading... %s",
						formatBytesCmd(p.BytesDownloaded))
				}
			}
		}()

		installed, err := downloader.Download(client, model, version, cfg.ComfyUIPath, progressCh, nil)
		close(progressCh)
		fmt.Println() // newline after progress

		if err != nil {
			fmt.Printf("  ERROR downloading: %v — skipping\n", err)
			continue
		}

		if err := trk.Add(*installed); err != nil {
			fmt.Printf("  ERROR tracking model: %v\n", err)
			continue
		}

		fmt.Printf("  Installed %s (%s)\n", installed.ModelName, installed.VersionName)
	}

	fmt.Println("\nImport complete.")
	return nil
}

func formatBytesCmd(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
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

	// Civitai API key
	currentKey := cfg.APIKey
	envKey := os.Getenv("CIVITAI_API_KEY")
	if envKey != "" {
		fmt.Println("Civitai API key is set via CIVITAI_API_KEY environment variable.")
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

	// HuggingFace token
	currentHF := cfg.HFToken
	hfEnv := os.Getenv("HF_TOKEN")
	if hfEnv != "" {
		fmt.Println("HuggingFace token is set via HF_TOKEN environment variable.")
	} else {
		display := "(not set)"
		if currentHF != "" {
			display = currentHF[:4] + "..." + currentHF[len(currentHF)-4:]
		}
		fmt.Printf("HuggingFace token [%s]: ", display)
		line, _ = reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" {
			cfg.HFToken = line
		}
	}

	return cfg.Save()
}
