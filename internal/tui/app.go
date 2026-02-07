package tui

import (
	"fmt"
	"strings"

	"github.com/burritocatai/civcat/internal/api"
	"github.com/burritocatai/civcat/internal/config"
	"github.com/burritocatai/civcat/internal/downloader"
	"github.com/burritocatai/civcat/internal/tracker"
	tea "github.com/charmbracelet/bubbletea"
)

type view int

const (
	viewInstalled view = iota
	viewSearch
	viewModelDetail
	viewConfig
	viewUpdates
)

type App struct {
	cfg     *config.Config
	client  *api.Client
	tracker *tracker.Tracker

	currentView  view
	width        int
	height       int

	// Installed view
	installedModels []tracker.InstalledModel
	installedCursor int

	// Search view
	searchQuery   string
	searchResults []api.Model
	searchCursor  int
	searchPage    int
	searchTotal   int
	searching     bool
	searchInput   bool

	// Model detail view
	detailModel   *api.Model
	detailCursor  int
	prevView      view

	// Updates view
	checkingUpdates bool
	updatesChecked  bool
	updateResults   []updateResult

	// Download state
	downloading    bool
	downloadName   string
	downloadPct    float64

	// Config view
	configCursor int
	configEdit   int // -1 = not editing, 0 = path, 1 = apikey
	configInput  string
	firstRun     bool

	// Status / error
	statusMsg string
	errMsg    string
}

type updateResult struct {
	Model         tracker.InstalledModel
	LatestVersion int
	LatestName    string
	HasUpdate     bool
}

// Messages
type searchResultMsg struct {
	results *api.ModelsResponse
	err     error
}

type modelDetailMsg struct {
	model *api.Model
	err   error
}

type downloadProgressMsg struct {
	progress downloader.Progress
}

type downloadCompleteMsg struct {
	installed *tracker.InstalledModel
	err       error
}

type updateCheckMsg struct {
	results []updateResult
	err     error
}

func NewApp(cfg *config.Config, client *api.Client, trk *tracker.Tracker) *App {
	a := &App{
		cfg:             cfg,
		client:          client,
		tracker:         trk,
		currentView:     viewInstalled,
		installedModels: trk.GetAll(),
		configEdit:      -1,
	}

	// First-run: open config view with path field in edit mode.
	if !cfg.IsConfigured() {
		a.firstRun = true
		a.currentView = viewConfig
		a.configCursor = 0
		a.configEdit = 0
		a.configInput = ""
	}

	return a
}

func (a *App) Init() tea.Cmd {
	return nil
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		return a, nil

	case tea.KeyMsg:
		// Global keys
		switch msg.String() {
		case "ctrl+c", "q":
			if a.searchInput || a.configEdit >= 0 {
				// Don't quit while editing
			} else {
				return a, tea.Quit
			}
		}

		return a.handleKeyPress(msg)

	case searchResultMsg:
		a.searching = false
		if msg.err != nil {
			a.errMsg = msg.err.Error()
		} else {
			a.searchResults = msg.results.Items
			a.searchTotal = msg.results.Metadata.TotalItems
			a.searchCursor = 0
			a.errMsg = ""
		}
		return a, nil

	case modelDetailMsg:
		if msg.err != nil {
			a.errMsg = msg.err.Error()
		} else {
			a.detailModel = msg.model
			a.detailCursor = 0
			a.currentView = viewModelDetail
			a.errMsg = ""
		}
		return a, nil

	case downloadProgressMsg:
		if msg.progress.TotalBytes > 0 {
			a.downloadPct = float64(msg.progress.BytesDownloaded) / float64(msg.progress.TotalBytes) * 100
		}
		return a, nil

	case downloadCompleteMsg:
		a.downloading = false
		if msg.err != nil {
			a.errMsg = fmt.Sprintf("Download failed: %v", msg.err)
		} else {
			a.tracker.Add(*msg.installed)
			a.installedModels = a.tracker.GetAll()
			a.statusMsg = fmt.Sprintf("Installed %s", msg.installed.ModelName)
			a.errMsg = ""
		}
		return a, nil

	case updateCheckMsg:
		a.checkingUpdates = false
		a.updatesChecked = true
		if msg.err != nil {
			a.errMsg = msg.err.Error()
		} else {
			a.updateResults = msg.results
			// Persist update flags.
			for _, r := range msg.results {
				if r.HasUpdate {
					a.tracker.MarkUpdate(r.Model.ModelID, r.LatestVersion)
				}
			}
			a.installedModels = a.tracker.GetAll()
			a.errMsg = ""
		}
		return a, nil
	}

	return a, nil
}

func (a *App) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle text input modes first.
	if a.searchInput {
		return a.handleSearchInput(msg)
	}
	if a.configEdit >= 0 {
		return a.handleConfigInput(msg)
	}

	switch a.currentView {
	case viewInstalled:
		return a.handleInstalledKeys(msg)
	case viewSearch:
		return a.handleSearchKeys(msg)
	case viewModelDetail:
		return a.handleDetailKeys(msg)
	case viewConfig:
		return a.handleConfigKeys(msg)
	case viewUpdates:
		return a.handleUpdatesKeys(msg)
	}
	return a, nil
}

func (a *App) handleInstalledKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if a.installedCursor > 0 {
			a.installedCursor--
		}
	case "down", "j":
		if a.installedCursor < len(a.installedModels)-1 {
			a.installedCursor++
		}
	case "s":
		a.currentView = viewSearch
		a.searchInput = true
		a.searchQuery = ""
	case "c":
		a.currentView = viewConfig
		a.configCursor = 0
	case "u":
		a.currentView = viewUpdates
		a.checkingUpdates = true
		a.updatesChecked = false
		return a, a.checkUpdatesCmd()
	case "enter":
		if len(a.installedModels) > 0 {
			m := a.installedModels[a.installedCursor]
			a.prevView = viewInstalled
			return a, a.fetchModelDetail(m.ModelID)
		}
	case "d":
		if len(a.installedModels) > 0 {
			m := a.installedModels[a.installedCursor]
			a.tracker.Remove(m.ModelID)
			a.installedModels = a.tracker.GetAll()
			if a.installedCursor >= len(a.installedModels) && a.installedCursor > 0 {
				a.installedCursor--
			}
			a.statusMsg = fmt.Sprintf("Removed %s from tracking", m.ModelName)
		}
	}
	return a, nil
}

func (a *App) handleSearchKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if a.searchCursor > 0 {
			a.searchCursor--
		}
	case "down", "j":
		if a.searchCursor < len(a.searchResults)-1 {
			a.searchCursor++
		}
	case "enter":
		if len(a.searchResults) > 0 {
			m := a.searchResults[a.searchCursor]
			a.prevView = viewSearch
			return a, a.fetchModelDetail(m.ID)
		}
	case "/":
		a.searchInput = true
		a.searchQuery = ""
	case "n":
		if a.searchPage*20 < a.searchTotal {
			a.searchPage++
			return a, a.searchCmd()
		}
	case "p":
		if a.searchPage > 1 {
			a.searchPage--
			return a, a.searchCmd()
		}
	case "esc":
		a.currentView = viewInstalled
	}
	return a, nil
}

func (a *App) handleSearchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		a.searchInput = false
		a.searchPage = 1
		return a, a.searchCmd()
	case "esc":
		a.searchInput = false
		if len(a.searchResults) == 0 {
			a.currentView = viewInstalled
		}
	case "backspace":
		if len(a.searchQuery) > 0 {
			a.searchQuery = a.searchQuery[:len(a.searchQuery)-1]
		}
	default:
		if len(msg.String()) == 1 || msg.String() == " " {
			a.searchQuery += msg.String()
		}
	}
	return a, nil
}

func (a *App) handleDetailKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if a.detailModel == nil {
		return a, nil
	}
	switch msg.String() {
	case "esc":
		a.currentView = a.prevView
		a.detailModel = nil
	case "up", "k":
		if a.detailCursor > 0 {
			a.detailCursor--
		}
	case "down", "j":
		if a.detailCursor < len(a.detailModel.Versions)-1 {
			a.detailCursor++
		}
	case "enter", "i":
		if !a.downloading && a.detailModel != nil && len(a.detailModel.Versions) > 0 {
			version := a.detailModel.Versions[a.detailCursor]
			a.downloading = true
			a.downloadName = a.detailModel.Name
			a.downloadPct = 0
			return a, a.downloadCmd(a.detailModel, &version)
		}
	}
	return a, nil
}

func (a *App) handleConfigKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if a.firstRun {
			// Can't leave config until ComfyUI path is set.
			a.errMsg = "Please set a ComfyUI path before continuing"
			return a, nil
		}
		a.currentView = viewInstalled
	case "up", "k":
		if a.configCursor > 0 {
			a.configCursor--
		}
	case "down", "j":
		if a.configCursor < 1 {
			a.configCursor++
		}
	case "enter":
		a.configEdit = a.configCursor
		if a.configCursor == 0 {
			a.configInput = a.cfg.ComfyUIPath
		} else {
			a.configInput = a.cfg.APIKey
		}
	}
	return a, nil
}

func (a *App) handleConfigInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if a.configEdit == 0 {
			if a.configInput == "" {
				a.errMsg = "ComfyUI path cannot be empty"
				return a, nil
			}
			a.cfg.ComfyUIPath = a.configInput
			a.cfg.Save()
			a.errMsg = ""

			// During first run, advance to API key field automatically.
			if a.firstRun {
				a.configEdit = 1
				a.configCursor = 1
				a.configInput = ""
				a.statusMsg = "ComfyUI path saved"
				return a, nil
			}
		} else {
			a.cfg.APIKey = a.configInput
			a.client = api.NewClient(a.cfg.GetAPIKey())
			a.cfg.Save()

			// First run complete — go to main view.
			if a.firstRun {
				a.firstRun = false
				a.currentView = viewInstalled
				a.statusMsg = "Setup complete! Press 's' to search for models."
				a.errMsg = ""
				return a, nil
			}
		}
		a.configEdit = -1
		a.statusMsg = "Config saved"
		a.errMsg = ""
	case "esc":
		if a.firstRun && a.configEdit == 0 {
			// Can't skip path during first run.
			a.errMsg = "Please set a ComfyUI path before continuing"
			return a, nil
		}
		if a.firstRun && a.configEdit == 1 {
			// Allow skipping API key during first run.
			a.firstRun = false
			a.configEdit = -1
			a.currentView = viewInstalled
			a.cfg.Save()
			a.statusMsg = "Setup complete! Press 's' to search for models."
			a.errMsg = ""
			return a, nil
		}
		a.configEdit = -1
	case "backspace":
		if len(a.configInput) > 0 {
			a.configInput = a.configInput[:len(a.configInput)-1]
		}
	default:
		if len(msg.String()) == 1 || msg.String() == " " {
			a.configInput += msg.String()
		}
	}
	return a, nil
}

func (a *App) handleUpdatesKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.currentView = viewInstalled
	case "r":
		a.checkingUpdates = true
		a.updatesChecked = false
		return a, a.checkUpdatesCmd()
	}
	return a, nil
}

// Commands

func (a *App) searchCmd() tea.Cmd {
	a.searching = true
	query := a.searchQuery
	page := a.searchPage
	client := a.client
	return func() tea.Msg {
		results, err := client.SearchModels(query, "", page, 20)
		return searchResultMsg{results: results, err: err}
	}
}

func (a *App) fetchModelDetail(modelID int) tea.Cmd {
	client := a.client
	return func() tea.Msg {
		model, err := client.GetModel(modelID)
		return modelDetailMsg{model: model, err: err}
	}
}

func (a *App) downloadCmd(model *api.Model, version *api.ModelVersion) tea.Cmd {
	client := a.client
	comfyPath := a.cfg.ComfyUIPath
	m := *model
	v := *version
	return func() tea.Msg {
		installed, err := downloader.Download(client, &m, &v, comfyPath, nil)
		return downloadCompleteMsg{installed: installed, err: err}
	}
}

func (a *App) checkUpdatesCmd() tea.Cmd {
	models := a.tracker.GetAll()
	client := a.client
	return func() tea.Msg {
		var results []updateResult
		for _, m := range models {
			model, err := client.GetModel(m.ModelID)
			if err != nil {
				return updateCheckMsg{err: fmt.Errorf("checking %s: %w", m.ModelName, err)}
			}
			r := updateResult{
				Model: m,
			}
			if len(model.Versions) > 0 {
				latest := model.Versions[0]
				r.LatestVersion = latest.ID
				r.LatestName = latest.Name
				r.HasUpdate = latest.ID != m.VersionID
			}
			results = append(results, r)
		}
		return updateCheckMsg{results: results}
	}
}

// View

func (a *App) View() string {
	var b strings.Builder

	// Header
	header := titleStyle.Render(" civcat - Civitai Model Manager ")
	b.WriteString(header + "\n\n")

	switch a.currentView {
	case viewInstalled:
		b.WriteString(a.viewInstalled())
	case viewSearch:
		b.WriteString(a.viewSearch())
	case viewModelDetail:
		b.WriteString(a.viewDetail())
	case viewConfig:
		b.WriteString(a.viewConfig())
	case viewUpdates:
		b.WriteString(a.viewUpdatesView())
	}

	// Status bar
	b.WriteString("\n")
	if a.errMsg != "" {
		b.WriteString(errorStyle.Render("Error: " + a.errMsg))
		b.WriteString("\n")
	}
	if a.statusMsg != "" {
		b.WriteString(successStyle.Render(a.statusMsg))
		b.WriteString("\n")
	}
	if a.downloading {
		b.WriteString(warningStyle.Render(fmt.Sprintf("Downloading %s... %.1f%%", a.downloadName, a.downloadPct)))
		b.WriteString("\n")
	}

	return b.String()
}

func (a *App) viewInstalled() string {
	var b strings.Builder

	b.WriteString(subtitleStyle.Render("Installed Models") + "\n\n")

	if len(a.installedModels) == 0 {
		b.WriteString(mutedStyle.Render("  No models installed. Press 's' to search and install models.") + "\n")
	} else {
		for i, m := range a.installedModels {
			prefix := "  "
			style := normalItemStyle
			if i == a.installedCursor {
				prefix = "> "
				style = selectedStyle
			}

			line := fmt.Sprintf("%s%-40s %-12s %-10s %s",
				prefix,
				truncate(m.ModelName, 38),
				m.Type,
				m.BaseModel,
				m.Creator,
			)

			if m.HasUpdate {
				line += warningStyle.Render(" [UPDATE]")
			}

			b.WriteString(style.Render(line) + "\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  s: search  u: check updates  c: config  enter: details  d: remove  q: quit"))

	return b.String()
}

func (a *App) viewSearch() string {
	var b strings.Builder

	b.WriteString(subtitleStyle.Render("Search Models") + "\n\n")

	if a.searchInput {
		b.WriteString(inputStyle.Render(fmt.Sprintf("Search: %s_", a.searchQuery)) + "\n\n")
	} else {
		b.WriteString(mutedStyle.Render(fmt.Sprintf("  Query: %s (total: %d)", a.searchQuery, a.searchTotal)) + "\n\n")
	}

	if a.searching {
		b.WriteString(mutedStyle.Render("  Searching...") + "\n")
	} else if len(a.searchResults) == 0 && !a.searchInput {
		b.WriteString(mutedStyle.Render("  No results.") + "\n")
	} else {
		for i, m := range a.searchResults {
			prefix := "  "
			style := normalItemStyle
			if i == a.searchCursor {
				prefix = "> "
				style = selectedStyle
			}

			installed := ""
			if a.tracker.IsInstalled(m.ID) {
				installed = successStyle.Render(" [installed]")
			}

			line := fmt.Sprintf("%s%-40s %-12s %-15s %s",
				prefix,
				truncate(m.Name, 38),
				m.Type,
				m.Creator.Username,
				installed,
			)
			b.WriteString(style.Render(line) + "\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  /: new search  enter: details  n/p: next/prev page  esc: back"))

	return b.String()
}

func (a *App) viewDetail() string {
	var b strings.Builder

	if a.detailModel == nil {
		b.WriteString(mutedStyle.Render("  Loading...") + "\n")
		return b.String()
	}

	m := a.detailModel
	b.WriteString(subtitleStyle.Render(m.Name) + "\n")
	b.WriteString(mutedStyle.Render(fmt.Sprintf("  by %s | Type: %s | Downloads: %d | Rating: %.1f",
		m.Creator.Username, m.Type, m.Stats.DownloadCount, m.Stats.Rating)) + "\n")

	if len(m.Tags) > 0 {
		tags := strings.Join(m.Tags, ", ")
		if len(tags) > 80 {
			tags = tags[:77] + "..."
		}
		b.WriteString(mutedStyle.Render("  Tags: "+tags) + "\n")
	}

	b.WriteString("\n" + subtitleStyle.Render("  Versions:") + "\n\n")

	for i, v := range m.Versions {
		prefix := "  "
		style := normalItemStyle
		if i == a.detailCursor {
			prefix = "> "
			style = selectedStyle
		}

		fileInfo := ""
		if len(v.Files) > 0 {
			f := v.Files[0]
			fileInfo = fmt.Sprintf("%.1f MB, %s", f.SizeKB/1024, f.Metadata.Format)
		}

		installed := ""
		im := a.tracker.GetByModelID(m.ID)
		if im != nil && im.VersionID == v.ID {
			installed = successStyle.Render(" [installed]")
		}

		line := fmt.Sprintf("%s%-30s %-15s %-20s%s",
			prefix,
			truncate(v.Name, 28),
			v.BaseModel,
			fileInfo,
			installed,
		)
		b.WriteString(style.Render(line) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  enter/i: install version  esc: back"))

	return b.String()
}

func (a *App) viewConfig() string {
	var b strings.Builder

	if a.firstRun {
		b.WriteString(subtitleStyle.Render("Welcome to civcat!") + "\n")
		b.WriteString(mutedStyle.Render("  Let's get you set up. Enter your ComfyUI installation path.") + "\n\n")
	} else {
		b.WriteString(subtitleStyle.Render("Configuration") + "\n\n")
	}

	fields := []struct {
		label string
		value string
	}{
		{"ComfyUI Path", a.cfg.ComfyUIPath},
		{"API Key", maskKey(a.cfg.GetAPIKey())},
	}

	for i, f := range fields {
		prefix := "  "
		style := normalItemStyle
		if i == a.configCursor {
			prefix = "> "
			style = selectedStyle
		}

		if a.configEdit == i {
			b.WriteString(style.Render(fmt.Sprintf("%s%s: ", prefix, f.label)))
			b.WriteString(inputStyle.Render(a.configInput+"_") + "\n")
		} else {
			val := f.value
			if val == "" {
				val = mutedStyle.Render("(not set)")
			}
			b.WriteString(style.Render(fmt.Sprintf("%s%-15s %s", prefix, f.label+":", val)) + "\n")
		}
	}

	b.WriteString("\n")
	envKey := ""
	if k := a.cfg.GetAPIKey(); k != "" && a.cfg.APIKey == "" {
		envKey = successStyle.Render("  API key loaded from CIVITAI_API_KEY env var")
	}
	if envKey != "" {
		b.WriteString(envKey + "\n")
	}

	b.WriteString("\n")
	if a.firstRun {
		if a.configEdit == 0 {
			b.WriteString(helpStyle.Render("  enter: save path"))
		} else if a.configEdit == 1 {
			b.WriteString(helpStyle.Render("  enter: save key  esc: skip (optional)"))
		} else {
			b.WriteString(helpStyle.Render("  enter: edit"))
		}
	} else {
		b.WriteString(helpStyle.Render("  enter: edit  esc: back"))
	}

	return b.String()
}

func (a *App) viewUpdatesView() string {
	var b strings.Builder

	b.WriteString(subtitleStyle.Render("Update Check") + "\n\n")

	if a.checkingUpdates {
		b.WriteString(mutedStyle.Render("  Checking for updates...") + "\n")
		return b.String()
	}

	if !a.updatesChecked {
		b.WriteString(mutedStyle.Render("  Press 'r' to check for updates.") + "\n")
		return b.String()
	}

	hasUpdates := false
	for _, r := range a.updateResults {
		status := successStyle.Render("up to date")
		if r.HasUpdate {
			status = warningStyle.Render(fmt.Sprintf("update available: %s", r.LatestName))
			hasUpdates = true
		}
		b.WriteString(fmt.Sprintf("  %-40s %s\n", truncate(r.Model.ModelName, 38), status))
	}

	if len(a.updateResults) == 0 {
		b.WriteString(mutedStyle.Render("  No models installed to check.") + "\n")
	} else if !hasUpdates {
		b.WriteString("\n" + successStyle.Render("  All models are up to date!") + "\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  r: refresh  esc: back"))

	return b.String()
}

// Helpers

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func maskKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}
