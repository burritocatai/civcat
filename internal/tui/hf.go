package tui

import (
	"fmt"
	"strings"

	"github.com/burritocatai/civcat/internal/hfapi"
	tea "github.com/charmbracelet/bubbletea"
)

// Key handlers

func (a *App) handleHFSearchKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if a.hfSearchCursor > 0 {
			a.hfSearchCursor--
		}
	case "down", "j":
		if a.hfSearchCursor < len(a.hfSearchResults)-1 {
			a.hfSearchCursor++
		}
	case "enter":
		if len(a.hfSearchResults) > 0 {
			m := a.hfSearchResults[a.hfSearchCursor]
			return a, a.hfFetchModelDetail(m.ID)
		}
	case "/":
		a.hfSearchInput = true
		a.hfSearchQuery = ""
	case "t":
		a.hfFilterIdx = (a.hfFilterIdx + 1) % len(hfFilters)
		return a, a.hfReSearch()
	case "T":
		a.hfFilterIdx = (a.hfFilterIdx + len(hfFilters) - 1) % len(hfFilters)
		return a, a.hfReSearch()
	case "o":
		a.hfSortIdx = (a.hfSortIdx + 1) % len(hfSorts)
		return a, a.hfReSearch()
	case "O":
		a.hfSortIdx = (a.hfSortIdx + len(hfSorts) - 1) % len(hfSorts)
		return a, a.hfReSearch()
	case "esc":
		a.currentView = viewInstalled
	}
	return a, nil
}

func (a *App) handleHFSearchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		a.hfSearchInput = false
		return a, a.hfSearchCmd()
	case "esc":
		a.hfSearchInput = false
		if len(a.hfSearchResults) == 0 {
			a.currentView = viewInstalled
		}
	case "backspace":
		if len(a.hfSearchQuery) > 0 {
			a.hfSearchQuery = a.hfSearchQuery[:len(a.hfSearchQuery)-1]
		}
	default:
		r := msg.Runes
		if len(r) > 0 {
			a.hfSearchQuery += string(r)
		}
	}
	return a, nil
}

func (a *App) handleHFDetailKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if a.hfDetailModel == nil {
		return a, nil
	}
	switch msg.String() {
	case "esc":
		a.currentView = viewHFSearch
		a.hfDetailModel = nil
	case "up", "k":
		if a.hfDetailCursor > 0 {
			a.hfDetailCursor--
			if a.hfDetailCursor < a.hfDetailOffset {
				a.hfDetailOffset = a.hfDetailCursor
			}
		}
	case "down", "j":
		if a.hfDetailCursor < len(a.hfDetailFiles)-1 {
			a.hfDetailCursor++
			maxVisible := a.hfFilePageSize()
			if maxVisible > 0 && a.hfDetailCursor >= a.hfDetailOffset+maxVisible {
				a.hfDetailOffset = a.hfDetailCursor - maxVisible + 1
			}
		}
	case "t":
		a.hfDetailTypeIdx = (a.hfDetailTypeIdx + 1) % len(hfModelTypes)
	case "T":
		a.hfDetailTypeIdx = (a.hfDetailTypeIdx + len(hfModelTypes) - 1) % len(hfModelTypes)
	case "enter", "i":
		if len(a.hfDetailFiles) > 0 {
			if a.hfDetailModel.IsGated() {
				a.errMsg = "This model is gated — you may need an HF token with access approval"
			}
			file := a.hfDetailFiles[a.hfDetailCursor]
			modelType := hfModelTypes[a.hfDetailTypeIdx].modelType
			name := a.hfDetailModel.ID + "/" + file.RFilename
			a.enqueue(queueItem{
				source:     sourceHuggingFace,
				name:       name,
				hfModel:    a.hfDetailModel,
				hfFilename: file.RFilename,
				hfType:     modelType,
			})
			a.statusMsg = fmt.Sprintf("Queued %s for download", name)
			return a, func() tea.Msg { return queueUpdatedMsg{} }
		}
	}
	return a, nil
}

// Commands

func (a *App) hfReSearch() tea.Cmd {
	if a.hfSearchQuery == "" {
		return nil
	}
	return a.hfSearchCmd()
}

func (a *App) hfSearchCmd() tea.Cmd {
	a.hfSearching = true
	p := hfapi.SearchParams{
		Query:     a.hfSearchQuery,
		Filter:    hfFilters[a.hfFilterIdx].value,
		Sort:      hfSorts[a.hfSortIdx].value,
		Direction: "-1",
		Limit:     20,
	}
	client := a.hfClient
	return func() tea.Msg {
		results, err := client.SearchModels(p)
		return hfSearchResultMsg{results: results, err: err}
	}
}

func (a *App) hfFetchModelDetail(repoID string) tea.Cmd {
	a.hfSearching = true
	client := a.hfClient
	return func() tea.Msg {
		model, err := client.GetModel(repoID)
		return hfModelDetailMsg{model: model, err: err}
	}
}

// Views

func (a *App) viewHFSearch() string {
	var b strings.Builder

	b.WriteString(subtitleStyle.Render("Search HuggingFace") + "\n\n")

	// Filter bar
	filterLabel := hfFilters[a.hfFilterIdx].label
	sortLabel := hfSorts[a.hfSortIdx].label
	b.WriteString(mutedStyle.Render(fmt.Sprintf("  Filter: %-25s  Sort: %s", filterLabel, sortLabel)) + "\n")

	if a.hfSearchInput {
		b.WriteString(inputStyle.Render(fmt.Sprintf("Search: %s_", a.hfSearchQuery)) + "\n\n")
	} else {
		b.WriteString(mutedStyle.Render(fmt.Sprintf("  Query: %s (%d results)", a.hfSearchQuery, len(a.hfSearchResults))) + "\n\n")
	}

	if a.hfSearching {
		b.WriteString(mutedStyle.Render("  Searching...") + "\n")
	} else if len(a.hfSearchResults) == 0 && !a.hfSearchInput {
		b.WriteString(mutedStyle.Render("  No results.") + "\n")
	} else {
		for i, m := range a.hfSearchResults {
			prefix := "  "
			style := normalItemStyle
			if i == a.hfSearchCursor {
				prefix = "> "
				style = selectedStyle
			}

			installed := ""
			if a.tracker.IsInstalledByName(m.ID) {
				installed = successStyle.Render(" [installed]")
			}

			gated := ""
			if m.IsGated() {
				gated = warningStyle.Render(" [gated]")
			}

			line := fmt.Sprintf("%s%-45s %-15s %s%s",
				prefix,
				truncate(m.ID, 43),
				formatCount(m.Downloads),
				installed,
				gated,
			)
			b.WriteString(style.Render(line) + "\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  /: search  t: filter  o: sort  enter: details  esc: back"))

	return b.String()
}

// hfFilePageSize returns the number of file lines visible in the viewport.
// It reserves space for the header, model info, and footer chrome.
func (a *App) hfFilePageSize() int {
	if a.height <= 0 {
		return 0 // unknown height — show all
	}
	// Overhead: app title(2) + model info(~7) + "Files:" header(3) + help/status(5)
	const overhead = 17
	avail := a.height - overhead
	if avail < 5 {
		avail = 5
	}
	return avail
}

func (a *App) viewHFDetail() string {
	var b strings.Builder

	if a.hfDetailModel == nil {
		b.WriteString(mutedStyle.Render("  Loading...") + "\n")
		return b.String()
	}

	m := a.hfDetailModel
	b.WriteString(subtitleStyle.Render(m.ID) + "\n")
	b.WriteString(mutedStyle.Render(fmt.Sprintf("  by %s | Downloads: %s | Likes: %s",
		m.Author, formatCount(m.Downloads), formatCount(m.Likes))) + "\n")

	if m.PipelineTag != "" {
		b.WriteString(mutedStyle.Render("  Pipeline: "+m.PipelineTag) + "\n")
	}

	if len(m.Tags) > 0 {
		tags := strings.Join(m.Tags, ", ")
		if len(tags) > 80 {
			tags = tags[:77] + "..."
		}
		b.WriteString(mutedStyle.Render("  Tags: "+tags) + "\n")
	}

	if m.IsGated() {
		b.WriteString(warningStyle.Render("  This model is gated — access approval may be required") + "\n")
	}

	typeLabel := hfModelTypes[a.hfDetailTypeIdx].label
	b.WriteString("\n" + mutedStyle.Render(fmt.Sprintf("  Install as: %s (t/T to change)", typeLabel)) + "\n")

	total := len(a.hfDetailFiles)
	filesLabel := "  Files:"
	if total > 0 {
		filesLabel = fmt.Sprintf("  Files: (%d total)", total)
	}
	b.WriteString("\n" + subtitleStyle.Render(filesLabel) + "\n\n")

	if total == 0 {
		b.WriteString(mutedStyle.Render("  No downloadable model files found.") + "\n")
	} else {
		maxVisible := a.hfFilePageSize()

		// Clamp offset.
		if a.hfDetailOffset > total-maxVisible {
			a.hfDetailOffset = total - maxVisible
		}
		if a.hfDetailOffset < 0 {
			a.hfDetailOffset = 0
		}

		start := a.hfDetailOffset
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
			f := a.hfDetailFiles[i]
			prefix := "  "
			style := normalItemStyle
			if i == a.hfDetailCursor {
				prefix = "> "
				style = selectedStyle
			}

			line := fmt.Sprintf("%s%s", prefix, f.RFilename)
			b.WriteString(style.Render(line) + "\n")
		}

		remaining := total - end
		if remaining > 0 {
			b.WriteString(mutedStyle.Render(fmt.Sprintf("  ... %d more below ...", remaining)) + "\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  enter/i: download file  t: change type  esc: back"))

	return b.String()
}

// formatCount formats a number with K/M suffixes for display.
func formatCount(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}
