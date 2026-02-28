package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/burritocatai/civcat/internal/api"
	"github.com/burritocatai/civcat/internal/config"
	"github.com/burritocatai/civcat/internal/hfapi"
	"github.com/burritocatai/civcat/internal/tracker"
	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
)

type view int

const (
	viewInstalled view = iota
	viewSearch
	viewModelDetail
	viewConfig
	viewUpdates
	viewHFSearch
	viewHFDetail
	viewFileSelect
)

type App struct {
	cfg      *config.Config
	client   *api.Client
	hfClient *hfapi.Client
	tracker  *tracker.Tracker

	currentView  view
	width        int
	height       int

	// Installed view
	installedModels     []tracker.InstalledModel
	installedCursor     int
	installedOffset     int // scroll offset for viewport
	installedFilterIdx  int // index into searchTypes for type filter

	// Search view
	searchQuery     string
	searchResults   []api.Model
	searchCursor    int
	searchNextPage  string // nextPage URL for cursor-based pagination
	searchHasNext   bool
	searchPageNum   int // display-only page counter
	searchTotal     int
	searching       bool
	searchInput     bool
	searchTypeIdx   int // index into searchTypes for category filter
	searchSortIdx   int // index into searchSorts
	searchBaseIdx   int // index into searchBaseModels

	// Model detail view
	detailModel   *api.Model
	detailCursor  int
	prevView      view

	// File selection view (for Civitai versions with multiple files)
	fileSelectVersion *api.ModelVersion
	fileSelectFiles   []api.ModelFile
	fileSelectCursor  int

	// Updates view
	checkingUpdates bool
	updatesChecked  bool
	updateResults   []updateResult

	// HuggingFace search view
	hfSearchQuery   string
	hfSearchResults []hfapi.HFModel
	hfSearchCursor  int
	hfSearching     bool
	hfSearchInput   bool
	hfFilterIdx     int // index into hfFilters for pipeline tag filter
	hfSortIdx       int // index into hfSorts

	// HuggingFace detail view
	hfDetailModel   *hfapi.HFModel
	hfDetailFiles   []hfapi.Sibling // downloadable files only
	hfDetailCursor  int
	hfDetailOffset  int // scroll offset for file list viewport
	hfDetailTypeIdx int // model type for ComfyUI directory mapping

	// Download queue
	queue          []queueItem
	active         []*activeDownload
	nextDownloadID int

	// Config view
	configCursor int
	configEdit   int // -1 = not editing, 0 = path, 1 = apikey
	configInput  string // -1 = not editing, 0 = path, 1 = apikey, 2 = hftoken
	firstRun     bool

	// Delete confirmation
	confirmDelete    bool
	deleteCandidate  *tracker.InstalledModel

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
	downloadID      int
	bytesDownloaded int64
	totalBytes      int64
}

type downloadCompleteMsg struct {
	downloadID int
	installed  *tracker.InstalledModel
	err        error
}

// searchTypes is the list of types the user can cycle through with 't'.
// Empty string means "All types".
var searchTypes = []struct {
	label     string
	modelType api.ModelType
}{
	{"All", ""},
	{"Checkpoint", api.ModelTypeCheckpoint},
	{"LORA", api.ModelTypeLORA},
	{"Embedding", api.ModelTypeTextualInversion},
	{"Controlnet", api.ModelTypeControlnet},
	{"VAE", api.ModelTypeVAE},
	{"Upscaler", api.ModelTypeUpscaler},
	{"Hypernetwork", api.ModelTypeHypernetwork},
	{"Poses", api.ModelTypePoses},
	{"Wildcards", api.ModelTypeWildcards},
	{"MotionModule", api.ModelTypeMotionModule},
	{"Workflows", api.ModelTypeWorkflows},
	{"Other", api.ModelTypeOther},
}

var searchSorts = []struct {
	label string
	value string
}{
	{"Most Downloaded", "Most Downloaded"},
	{"Highest Rated", "Highest Rated"},
	{"Newest", "Newest"},
}

var searchBaseModels = []struct {
	label string
	value string
}{
	{"All", ""},
	// Flux
	{"Flux.1 D", "Flux.1 D"},
	{"Flux.1 S", "Flux.1 S"},
	{"Flux.1 Kontext", "Flux.1 Kontext"},
	{"Flux.2 D", "Flux.2 D"},
	// Pony
	{"Pony", "Pony"},
	{"Pony V7", "Pony V7"},
	// SDXL
	{"SDXL 1.0", "SDXL 1.0"},
	{"SDXL 1.0 LCM", "SDXL 1.0 LCM"},
	{"SDXL Turbo", "SDXL Turbo"},
	{"SDXL Lightning", "SDXL Lightning"},
	{"SDXL Hyper", "SDXL Hyper"},
	{"SDXL Distilled", "SDXL Distilled"},
	// SD 3.x
	{"SD 3", "SD 3"},
	{"SD 3.5", "SD 3.5"},
	{"SD 3.5 Large", "SD 3.5 Large"},
	{"SD 3.5 Large Turbo", "SD 3.5 Large Turbo"},
	{"SD 3.5 Medium", "SD 3.5 Medium"},
	// SD 1.x
	{"SD 1.5", "SD 1.5"},
	{"SD 1.5 LCM", "SD 1.5 LCM"},
	{"SD 1.5 Hyper", "SD 1.5 Hyper"},
	{"SD 1.4", "SD 1.4"},
	// SD 2.x
	{"SD 2.1", "SD 2.1"},
	{"SD 2.1 768", "SD 2.1 768"},
	// Illustrious / NoobAI
	{"Illustrious", "Illustrious"},
	{"NoobAI", "NoobAI"},
	{"Chroma", "Chroma"},
	// Video models
	{"Hunyuan Video", "Hunyuan Video"},
	{"Wan Video", "Wan Video"},
	{"SVD", "SVD"},
	{"SVD XT", "SVD XT"},
	{"CogVideoX", "CogVideoX"},
	{"LTXV", "LTXV"},
	{"Mochi", "Mochi"},
	// Other
	{"Hunyuan 1", "Hunyuan 1"},
	{"Kolors", "Kolors"},
	{"Stable Cascade", "Stable Cascade"},
	{"AuraFlow", "AuraFlow"},
	{"PixArt a", "PixArt a"},
	{"Other", "Other"},
}

// HuggingFace messages
type hfSearchResultMsg struct {
	results []hfapi.HFModel
	err     error
}

type hfModelDetailMsg struct {
	model *hfapi.HFModel
	err   error
}

// hfFilters is the list of pipeline tag filters for HF search.
var hfFilters = []struct {
	label string
	value string
}{
	{"All", ""},
	{"Text-to-Image", "text-to-image"},
	{"Image-to-Image", "image-to-image"},
	{"Image-to-Video", "image-to-video"},
	{"Text-to-Video", "text-to-video"},
	{"Unconditional Generation", "unconditional-image-generation"},
	{"Image Upscaling", "image-super-resolution"},
	{"Inpainting", "image-inpainting"},
	{"Image Classification", "image-classification"},
	{"Depth Estimation", "depth-estimation"},
	{"Image Segmentation", "image-segmentation"},
}

var hfSorts = []struct {
	label string
	value string
}{
	{"Most Downloads", "downloads"},
	{"Most Likes", "likes"},
	{"Recently Updated", "lastModified"},
}

// hfModelTypes maps selectable types for ComfyUI directory placement.
var hfModelTypes = []struct {
	label     string
	modelType api.ModelType
}{
	{"Checkpoint", api.ModelTypeCheckpoint},
	{"LORA", api.ModelTypeLORA},
	{"VAE", api.ModelTypeVAE},
	{"Text Encoders", api.ModelTypeTextEncoders},
	{"Diffusion Models", api.ModelTypeDiffusionModels},
	{"CLIP Vision", api.ModelTypeClipVision},
	{"Style Models", api.ModelTypeStyleModels},
	{"Embedding", api.ModelTypeTextualInversion},
	{"Diffusers", api.ModelTypeDiffusers},
	{"VAE Approx", api.ModelTypeVAEApprox},
	{"Controlnet", api.ModelTypeControlnet},
	{"Gligen", api.ModelTypeGligen},
	{"Upscaler", api.ModelTypeUpscaler},
	{"Latent Upscaler", api.ModelTypeLatentUpscale},
	{"Hypernetwork", api.ModelTypeHypernetwork},
	{"Photomaker", api.ModelTypePhotomaker},
	{"Classifiers", api.ModelTypeClassifiers},
	{"Model Patches", api.ModelTypeModelPatches},
	{"Audio Encoders", api.ModelTypeAudioEncoders},
	{"CLIP", api.ModelTypeCLIP},
	{"SAMs", api.ModelTypeSAMs},
	{"UNet", api.ModelTypeUNet},
	{"Ultralytics Bbox", api.ModelTypeUltralyticsBbox},
	{"Ultralytics Segm", api.ModelTypeUltralyticsSegm},
	{"Other", api.ModelTypeOther},
}

type updateCheckMsg struct {
	results []updateResult
	err     error
}

func NewApp(cfg *config.Config, client *api.Client, hfClient *hfapi.Client, trk *tracker.Tracker) *App {
	a := &App{
		cfg:             cfg,
		client:          client,
		hfClient:        hfClient,
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
		for _, ad := range a.active {
			ad.progress.Width = msg.Width - 6
			if ad.progress.Width > 80 {
				ad.progress.Width = 80
			}
		}
		return a, nil

	case tea.KeyMsg:
		// Global keys
		switch msg.String() {
		case "ctrl+c", "q":
			if a.searchInput || a.hfSearchInput || a.configEdit >= 0 || a.confirmDelete {
				// Don't quit while editing or confirming
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
			a.searchNextPage = msg.results.Metadata.NextPage
			a.searchHasNext = msg.results.Metadata.NextPage != ""
			a.searchCursor = 0
			a.errMsg = ""
		}
		return a, nil

	case modelDetailMsg:
		a.searching = false
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
		ad := a.findActive(msg.downloadID)
		if ad != nil {
			ad.bytes = msg.bytesDownloaded
			ad.total = msg.totalBytes
			if msg.totalBytes > 0 {
				ad.pct = float64(msg.bytesDownloaded) / float64(msg.totalBytes)
			}
			cmd := ad.progress.SetPercent(ad.pct)
			return a, tea.Batch(cmd, waitForProgressByID(ad.id, ad.ch))
		}
		return a, nil

	case downloadCompleteMsg:
		ad := a.findActive(msg.downloadID)
		if ad != nil {
			a.removeActive(msg.downloadID)
		}
		if msg.err != nil {
			a.errMsg = fmt.Sprintf("Download failed: %v", msg.err)
		} else {
			a.tracker.Add(*msg.installed)
			a.installedModels = a.tracker.GetAll()
			a.clampInstalledViewport(len(a.filteredInstalledModels()))
			a.statusMsg = fmt.Sprintf("Installed %s", msg.installed.ModelName)
			a.errMsg = ""
		}
		return a, a.dequeue()

	// Handle animated progress bar frames.
	case progress.FrameMsg:
		var cmds []tea.Cmd
		for _, ad := range a.active {
			m, cmd := ad.progress.Update(msg)
			ad.progress = m.(progress.Model)
			cmds = append(cmds, cmd)
		}
		return a, tea.Batch(cmds...)

	case queueUpdatedMsg:
		return a, a.dequeue()

	case hfSearchResultMsg:
		a.hfSearching = false
		if msg.err != nil {
			a.errMsg = msg.err.Error()
		} else {
			a.hfSearchResults = msg.results
			a.hfSearchCursor = 0
			a.errMsg = ""
		}
		return a, nil

	case hfModelDetailMsg:
		a.hfSearching = false
		if msg.err != nil {
			a.errMsg = msg.err.Error()
		} else {
			a.hfDetailModel = msg.model
			// Filter to only downloadable files.
			a.hfDetailFiles = nil
			for _, s := range msg.model.Siblings {
				if s.IsDownloadable() {
					a.hfDetailFiles = append(a.hfDetailFiles, s)
				}
			}
			a.hfDetailCursor = 0
			a.hfDetailOffset = 0
			a.hfDetailTypeIdx = 0
			a.currentView = viewHFDetail
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
			a.clampInstalledViewport(len(a.filteredInstalledModels()))
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
	if a.hfSearchInput {
		return a.handleHFSearchInput(msg)
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
	case viewHFSearch:
		return a.handleHFSearchKeys(msg)
	case viewHFDetail:
		return a.handleHFDetailKeys(msg)
	case viewFileSelect:
		return a.handleFileSelectKeys(msg)
	}
	return a, nil
}

func (a *App) handleInstalledKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filtered := a.filteredInstalledModels()

	// Handle delete confirmation dialog.
	if a.confirmDelete {
		switch msg.String() {
		case "y", "Y":
			m := a.deleteCandidate
			// Delete the model file from disk.
			var fileErr error
			if m.FilePath != "" {
				fileErr = os.Remove(m.FilePath)
			}
			// Remove from tracker regardless of file deletion result.
			if m.Source == "huggingface" {
				a.tracker.RemoveHF(m.ModelName, m.FileName)
			} else {
				a.tracker.Remove(m.ModelID)
			}
			a.installedModels = a.tracker.GetAll()
			filtered = a.filteredInstalledModels()
			a.clampInstalledViewport(len(filtered))
			if fileErr != nil && !os.IsNotExist(fileErr) {
				a.statusMsg = fmt.Sprintf("Removed %s from tracking (file delete failed: %v)", m.ModelName, fileErr)
			} else {
				a.statusMsg = fmt.Sprintf("Deleted %s and removed from tracking", m.ModelName)
			}
			a.errMsg = ""
		default:
			a.statusMsg = "Delete cancelled"
		}
		a.confirmDelete = false
		a.deleteCandidate = nil
		return a, nil
	}

	switch msg.String() {
	case "up", "k":
		if a.installedCursor > 0 {
			a.installedCursor--
			if a.installedCursor < a.installedOffset {
				a.installedOffset = a.installedCursor
			}
		}
	case "down", "j":
		if a.installedCursor < len(filtered)-1 {
			a.installedCursor++
			maxVisible := a.installedPageSize()
			if maxVisible > 0 && a.installedCursor >= a.installedOffset+maxVisible {
				a.installedOffset = a.installedCursor - maxVisible + 1
			}
		}
	case "t":
		a.installedFilterIdx = (a.installedFilterIdx + 1) % len(searchTypes)
		a.installedCursor = 0
		a.installedOffset = 0
	case "T":
		a.installedFilterIdx = (a.installedFilterIdx + len(searchTypes) - 1) % len(searchTypes)
		a.installedCursor = 0
		a.installedOffset = 0
	case "s":
		a.currentView = viewSearch
		a.searchInput = true
		a.searchQuery = ""
	case "h":
		a.currentView = viewHFSearch
		a.hfSearchInput = true
		a.hfSearchQuery = ""
	case "c":
		a.currentView = viewConfig
		a.configCursor = 0
	case "u":
		a.currentView = viewUpdates
		a.checkingUpdates = true
		a.updatesChecked = false
		return a, a.checkUpdatesCmd()
	case "enter":
		if len(filtered) > 0 {
			m := filtered[a.installedCursor]
			a.prevView = viewInstalled
			return a, a.fetchModelDetail(m.ModelID)
		}
	case "d":
		if len(filtered) > 0 {
			m := filtered[a.installedCursor]
			a.confirmDelete = true
			a.deleteCandidate = &m
			a.statusMsg = ""
			a.errMsg = ""
		}
	case "e":
		if len(a.installedModels) > 0 {
			dir, err := config.ConfigDir()
			if err != nil {
				a.errMsg = fmt.Sprintf("Export failed: %v", err)
			} else {
				path := dir + "/models-export.json"
				if err := a.tracker.Export(path); err != nil {
					a.errMsg = fmt.Sprintf("Export failed: %v", err)
				} else {
					a.statusMsg = fmt.Sprintf("Exported %d model(s) to %s", len(a.installedModels), path)
					a.errMsg = ""
				}
			}
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
	case "t":
		a.searchTypeIdx = (a.searchTypeIdx + 1) % len(searchTypes)
		return a, a.reSearch()
	case "T":
		a.searchTypeIdx = (a.searchTypeIdx + len(searchTypes) - 1) % len(searchTypes)
		return a, a.reSearch()
	case "o":
		a.searchSortIdx = (a.searchSortIdx + 1) % len(searchSorts)
		return a, a.reSearch()
	case "O":
		a.searchSortIdx = (a.searchSortIdx + len(searchSorts) - 1) % len(searchSorts)
		return a, a.reSearch()
	case "b":
		a.searchBaseIdx = (a.searchBaseIdx + 1) % len(searchBaseModels)
		return a, a.reSearch()
	case "B":
		a.searchBaseIdx = (a.searchBaseIdx + len(searchBaseModels) - 1) % len(searchBaseModels)
		return a, a.reSearch()
	case "n":
		if a.searchHasNext && a.searchNextPage != "" {
			a.searchPageNum++
			return a, a.searchNextPageCmd()
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
		a.searchPageNum = 1
		a.searchNextPage = ""
		a.searchHasNext = false
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
		r := msg.Runes
		if len(r) > 0 {
			a.searchQuery += string(r)
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
		if a.detailModel != nil && len(a.detailModel.Versions) > 0 {
			version := a.detailModel.Versions[a.detailCursor]
			if version.IsEarlyAccess() {
				days := version.EarlyAccessDaysLeft()
				a.errMsg = fmt.Sprintf("This version is in early access (%d days remaining) — download unavailable", days)
				return a, nil
			}
			// If version has multiple files, show file selection view.
			if len(version.Files) > 1 {
				a.fileSelectVersion = &version
				a.fileSelectFiles = version.Files
				a.fileSelectCursor = 0
				a.currentView = viewFileSelect
				return a, nil
			}
			a.enqueue(queueItem{
				source:     sourceCivitai,
				name:       a.detailModel.Name,
				civModel:   a.detailModel,
				civVersion: &version,
			})
			a.statusMsg = fmt.Sprintf("Queued %s for download", a.detailModel.Name)
			return a, func() tea.Msg { return queueUpdatedMsg{} }
		}
	}
	return a, nil
}

func (a *App) handleFileSelectKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if a.fileSelectVersion == nil {
		return a, nil
	}
	switch msg.String() {
	case "esc":
		a.currentView = viewModelDetail
		a.fileSelectVersion = nil
		a.fileSelectFiles = nil
	case "up", "k":
		if a.fileSelectCursor > 0 {
			a.fileSelectCursor--
		}
	case "down", "j":
		if a.fileSelectCursor < len(a.fileSelectFiles)-1 {
			a.fileSelectCursor++
		}
	case "enter", "i":
		if len(a.fileSelectFiles) > 0 {
			file := a.fileSelectFiles[a.fileSelectCursor]
			version := a.fileSelectVersion
			a.enqueue(queueItem{
				source:     sourceCivitai,
				name:       a.detailModel.Name,
				civModel:   a.detailModel,
				civVersion: version,
				civFile:    &file,
			})
			a.statusMsg = fmt.Sprintf("Queued %s for download", a.detailModel.Name)
			return a, func() tea.Msg { return queueUpdatedMsg{} }
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
		if a.configCursor < 3 {
			a.configCursor++
		}
	case "enter":
		if a.configCursor == 3 {
			// Toggle parallel download (boolean, no text editing).
			a.cfg.ParallelDownload = !a.cfg.ParallelDownload
			a.cfg.Save()
			if a.cfg.ParallelDownload {
				a.statusMsg = "Parallel download enabled"
			} else {
				a.statusMsg = "Parallel download disabled"
			}
			return a, nil
		}
		a.configEdit = a.configCursor
		switch a.configCursor {
		case 0:
			a.configInput = a.cfg.ComfyUIPath
		case 1:
			a.configInput = a.cfg.APIKey
		case 2:
			a.configInput = a.cfg.HFToken
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
		} else if a.configEdit == 1 {
			a.cfg.APIKey = a.configInput
			a.client = api.NewClient(a.cfg.GetAPIKey())
			a.cfg.Save()

			// During first run, advance to HF token field.
			if a.firstRun {
				a.configEdit = 2
				a.configCursor = 2
				a.configInput = ""
				a.statusMsg = "Civitai API key saved"
				return a, nil
			}
		} else if a.configEdit == 2 {
			a.cfg.HFToken = a.configInput
			a.hfClient = hfapi.NewClient(a.cfg.GetHFToken())
			a.cfg.Save()

			// First run complete — go to main view.
			if a.firstRun {
				a.firstRun = false
				a.currentView = viewInstalled
				a.statusMsg = "Setup complete! Press 's' to search Civitai, 'h' to search HuggingFace."
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
		if a.firstRun && (a.configEdit == 1 || a.configEdit == 2) {
			// Allow skipping API keys during first run.
			a.firstRun = false
			a.configEdit = -1
			a.currentView = viewInstalled
			a.client = api.NewClient(a.cfg.GetAPIKey())
			a.hfClient = hfapi.NewClient(a.cfg.GetHFToken())
			a.cfg.Save()
			a.statusMsg = "Setup complete! Press 's' to search Civitai, 'h' to search HuggingFace."
			a.errMsg = ""
			return a, nil
		}
		a.configEdit = -1
	case "backspace":
		if len(a.configInput) > 0 {
			a.configInput = a.configInput[:len(a.configInput)-1]
		}
	default:
		r := msg.Runes
		if len(r) > 0 {
			a.configInput += string(r)
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

// parseCivitaiURN parses a Civitai AIR URN (e.g. "urn:air:sdxl:checkpoint:civitai:24350@2636109")
// and returns the model ID and version ID. Version ID is 0 if not present.
func parseCivitaiURN(s string) (modelID int, versionID int, ok bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "urn:air:") {
		return 0, 0, false
	}
	parts := strings.Split(s, ":")
	// urn:air:<base>:<type>:civitai:<modelID>@<versionID>
	if len(parts) != 6 || parts[4] != "civitai" {
		return 0, 0, false
	}
	idPart := parts[5]
	if idx := strings.Index(idPart, "@"); idx >= 0 {
		vid, err := strconv.Atoi(idPart[idx+1:])
		if err != nil {
			return 0, 0, false
		}
		versionID = vid
		idPart = idPart[:idx]
	}
	mid, err := strconv.Atoi(idPart)
	if err != nil {
		return 0, 0, false
	}
	return mid, versionID, true
}

// filteredInstalledModels returns the installed models matching the current type filter.
func (a *App) filteredInstalledModels() []tracker.InstalledModel {
	if a.installedFilterIdx == 0 { // "All"
		return a.installedModels
	}
	filterType := searchTypes[a.installedFilterIdx].modelType
	var filtered []tracker.InstalledModel
	for _, m := range a.installedModels {
		if m.Type == filterType {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

// installedPageSize returns the number of model lines visible in the viewport.
func (a *App) installedPageSize() int {
	if a.height <= 0 {
		return 0 // unknown height — show all
	}
	// Overhead: app title(2) + subtitle+blank(2) + filter bar(1) + blank(1)
	// + blank after list(1) + help(3 with padding) + status/error/downloads(~4)
	const overhead = 14
	avail := a.height - overhead
	if avail < 5 {
		avail = 5
	}
	return avail
}

// clampInstalledViewport ensures cursor and offset stay in bounds for the given list length.
func (a *App) clampInstalledViewport(listLen int) {
	if a.installedCursor >= listLen {
		a.installedCursor = listLen - 1
	}
	if a.installedCursor < 0 {
		a.installedCursor = 0
	}
	maxVisible := a.installedPageSize()
	if maxVisible <= 0 || maxVisible >= listLen {
		a.installedOffset = 0
		return
	}
	if a.installedCursor < a.installedOffset {
		a.installedOffset = a.installedCursor
	}
	if a.installedCursor >= a.installedOffset+maxVisible {
		a.installedOffset = a.installedCursor - maxVisible + 1
	}
	if a.installedOffset > listLen-maxVisible {
		a.installedOffset = listLen - maxVisible
	}
	if a.installedOffset < 0 {
		a.installedOffset = 0
	}
}

// Commands

// reSearch resets pagination and re-searches with current filters.
// Returns nil if no query has been entered yet.
func (a *App) reSearch() tea.Cmd {
	if a.searchQuery == "" {
		return nil
	}
	a.searchPageNum = 1
	a.searchNextPage = ""
	a.searchHasNext = false
	return a.searchCmd()
}

func (a *App) searchCmd() tea.Cmd {
	a.searching = true

	// Detect Civitai AIR URN and fetch the model directly.
	if modelID, _, ok := parseCivitaiURN(a.searchQuery); ok {
		client := a.client
		a.prevView = viewSearch
		return func() tea.Msg {
			model, err := client.GetModel(modelID)
			return modelDetailMsg{model: model, err: err}
		}
	}

	p := api.SearchParams{
		Query:     a.searchQuery,
		ModelType: searchTypes[a.searchTypeIdx].modelType,
		Sort:      searchSorts[a.searchSortIdx].value,
		BaseModel: searchBaseModels[a.searchBaseIdx].value,
		Limit:     20,
	}
	client := a.client
	return func() tea.Msg {
		results, err := client.SearchModels(p)
		return searchResultMsg{results: results, err: err}
	}
}

func (a *App) searchNextPageCmd() tea.Cmd {
	a.searching = true
	nextPage := a.searchNextPage
	client := a.client
	return func() tea.Msg {
		results, err := client.FetchPage(nextPage)
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

func (a *App) checkUpdatesCmd() tea.Cmd {
	models := a.tracker.GetAll()
	client := a.client
	return func() tea.Msg {
		var results []updateResult
		for _, m := range models {
			// Skip HuggingFace models — they have no Civitai version to check.
			if m.Source == "huggingface" {
				continue
			}
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
	case viewHFSearch:
		b.WriteString(a.viewHFSearch())
	case viewHFDetail:
		b.WriteString(a.viewHFDetail())
	case viewFileSelect:
		b.WriteString(a.viewFileSelect())
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
	b.WriteString(a.viewDownloadStatus())

	return b.String()
}

func (a *App) viewInstalled() string {
	var b strings.Builder

	filtered := a.filteredInstalledModels()
	total := len(filtered)

	b.WriteString(subtitleStyle.Render("Installed Models") + "\n\n")

	// Filter bar
	typeLabel := searchTypes[a.installedFilterIdx].label
	countLabel := fmt.Sprintf("%d models", total)
	if a.installedFilterIdx != 0 {
		countLabel = fmt.Sprintf("%d of %d models", total, len(a.installedModels))
	}
	b.WriteString(mutedStyle.Render(fmt.Sprintf("  Type: %-14s  %s", typeLabel, countLabel)) + "\n\n")

	if len(a.installedModels) == 0 {
		b.WriteString(mutedStyle.Render("  No models installed. Press 's' to search Civitai, 'h' for HuggingFace.") + "\n")
	} else if total == 0 {
		b.WriteString(mutedStyle.Render("  No models match the current filter.") + "\n")
	} else {
		a.clampInstalledViewport(total)

		maxVisible := a.installedPageSize()
		start := a.installedOffset
		end := total
		if maxVisible > 0 && maxVisible < total {
			end = start + maxVisible
			if end > total {
				end = total
			}
		}

		if start > 0 {
			b.WriteString(mutedStyle.Render(fmt.Sprintf("  ... %d more above ...", start)) + "\n")
		}

		for i := start; i < end; i++ {
			m := filtered[i]
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

			if m.Source == "huggingface" {
				line += mutedStyle.Render(" [HF]")
			}
			if m.HasUpdate {
				line += warningStyle.Render(" [UPDATE]")
			}

			b.WriteString(style.Render(line) + "\n")
		}

		remaining := total - end
		if remaining > 0 {
			b.WriteString(mutedStyle.Render(fmt.Sprintf("  ... %d more below ...", remaining)) + "\n")
		}
	}

	b.WriteString("\n")
	if a.confirmDelete && a.deleteCandidate != nil {
		b.WriteString(warningStyle.Render(fmt.Sprintf("  Delete %s and remove file from disk? (y/N)", a.deleteCandidate.ModelName)))
	} else {
		b.WriteString(helpStyle.Render("  s: search civitai  h: search huggingface  t: filter type  u: updates  c: config  enter: details  d: delete  e: export  q: quit"))
	}

	return b.String()
}

func (a *App) viewSearch() string {
	var b strings.Builder

	b.WriteString(subtitleStyle.Render("Search Models") + "\n\n")

	// Filter bar
	typeLabel := searchTypes[a.searchTypeIdx].label
	sortLabel := searchSorts[a.searchSortIdx].label
	baseLabel := searchBaseModels[a.searchBaseIdx].label
	b.WriteString(mutedStyle.Render(fmt.Sprintf("  Type: %-14s  Base: %-20s  Sort: %s", typeLabel, baseLabel, sortLabel)) + "\n")

	if a.searchInput {
		b.WriteString(inputStyle.Render(fmt.Sprintf("Search: %s_", a.searchQuery)) + "\n\n")
	} else {
		pageInfo := ""
		if a.searchPageNum > 0 {
			pageInfo = fmt.Sprintf(" | page %d", a.searchPageNum)
		}
		b.WriteString(mutedStyle.Render(fmt.Sprintf("  Query: %s (total: %d%s)", a.searchQuery, a.searchTotal, pageInfo)) + "\n\n")
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
	b.WriteString(helpStyle.Render("  /: search  t: type  b: base model  o: sort  n: next page  enter: details  esc: back"))

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
		if len(v.Files) > 1 {
			fileInfo = fmt.Sprintf("%d files", len(v.Files))
		} else if len(v.Files) == 1 {
			f := v.Files[0]
			fileInfo = fmt.Sprintf("%.1f MB, %s", f.SizeKB/1024, f.Metadata.Format)
		}

		badge := ""
		im := a.tracker.GetByModelID(m.ID)
		if im != nil && im.VersionID == v.ID {
			badge = successStyle.Render(" [installed]")
		}
		if v.IsEarlyAccess() {
			days := v.EarlyAccessDaysLeft()
			badge = warningStyle.Render(fmt.Sprintf(" [EARLY ACCESS - %dd]", days))
		}

		line := fmt.Sprintf("%s%-30s %-15s %-20s%s",
			prefix,
			truncate(v.Name, 28),
			v.BaseModel,
			fileInfo,
			badge,
		)
		b.WriteString(style.Render(line) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  enter/i: install version  esc: back"))

	return b.String()
}

func (a *App) viewFileSelect() string {
	var b strings.Builder

	if a.detailModel == nil || a.fileSelectVersion == nil {
		b.WriteString(mutedStyle.Render("  Loading...") + "\n")
		return b.String()
	}

	m := a.detailModel
	v := a.fileSelectVersion
	b.WriteString(subtitleStyle.Render(m.Name) + "\n")
	b.WriteString(mutedStyle.Render(fmt.Sprintf("  Version: %s | Base: %s", v.Name, v.BaseModel)) + "\n")
	b.WriteString("\n" + subtitleStyle.Render(fmt.Sprintf("  Select file: (%d available)", len(a.fileSelectFiles))) + "\n\n")

	for i, f := range a.fileSelectFiles {
		prefix := "  "
		style := normalItemStyle
		if i == a.fileSelectCursor {
			prefix = "> "
			style = selectedStyle
		}

		sizeMB := f.SizeKB / 1024
		sizeStr := fmt.Sprintf("%.1f MB", sizeMB)
		if sizeMB >= 1024 {
			sizeStr = fmt.Sprintf("%.1f GB", sizeMB/1024)
		}

		desc := f.Metadata.Format
		if f.Metadata.Size != "" {
			desc += ", " + f.Metadata.Size
		}
		if f.Metadata.FP != "" {
			desc += ", " + f.Metadata.FP
		}

		line := fmt.Sprintf("%s%-45s %s (%s)", prefix, truncate(f.Name, 43), sizeStr, desc)
		b.WriteString(style.Render(line) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  enter/i: download file  esc: back"))

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
		{"Civitai API Key", maskKey(a.cfg.GetAPIKey())},
		{"HF Token", maskKey(a.cfg.GetHFToken())},
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

	// Parallel download toggle (row index 3).
	{
		prefix := "  "
		style := normalItemStyle
		if a.configCursor == 3 {
			prefix = "> "
			style = selectedStyle
		}
		val := "Off"
		if a.cfg.ParallelDownload {
			val = "On"
		}
		b.WriteString(style.Render(fmt.Sprintf("%s%-15s %s", prefix, "Parallel DL:", val)) + "\n")
	}

	b.WriteString("\n")
	if k := a.cfg.GetAPIKey(); k != "" && a.cfg.APIKey == "" {
		b.WriteString(successStyle.Render("  Civitai API key loaded from CIVITAI_API_KEY env var") + "\n")
	}
	if k := a.cfg.GetHFToken(); k != "" && a.cfg.HFToken == "" {
		b.WriteString(successStyle.Render("  HF token loaded from HF_TOKEN env var") + "\n")
	}

	b.WriteString("\n")
	if a.firstRun {
		if a.configEdit == 0 {
			b.WriteString(helpStyle.Render("  enter: save path"))
		} else if a.configEdit == 1 || a.configEdit == 2 {
			b.WriteString(helpStyle.Render("  enter: save  esc: skip (optional)"))
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

func formatBytes(b int64) string {
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
