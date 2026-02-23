package tui

import (
	"fmt"

	"github.com/burritocatai/civcat/internal/api"
	"github.com/burritocatai/civcat/internal/downloader"
	"github.com/burritocatai/civcat/internal/hfapi"
	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
)

type downloadSource int

const (
	sourceCivitai downloadSource = iota
	sourceHuggingFace
)

// queueItem holds everything needed to start a download of either type.
type queueItem struct {
	source downloadSource
	name   string // display name

	// Civitai fields
	civModel   *api.Model
	civVersion *api.ModelVersion
	civFile    *api.ModelFile

	// HuggingFace fields
	hfModel    *hfapi.HFModel
	hfFilename string
	hfType     api.ModelType
}

// activeDownload tracks an in-progress download.
type activeDownload struct {
	id       int
	item     queueItem
	ch       chan downloader.Progress
	progress progress.Model
	pct      float64
	bytes    int64
	total    int64
}

// queueUpdatedMsg triggers dequeue after enqueue.
type queueUpdatedMsg struct{}

func (a *App) enqueue(item queueItem) {
	a.queue = append(a.queue, item)
}

// dequeue starts the next eligible download if capacity is available.
// In parallel mode, allows 1 Civitai + 1 HF simultaneously.
func (a *App) dequeue() tea.Cmd {
	if len(a.queue) == 0 {
		return nil
	}

	maxActive := 1
	if a.cfg.ParallelDownload {
		maxActive = 2
	}

	if len(a.active) >= maxActive {
		return nil
	}

	// In parallel mode, find an item whose source isn't already active.
	for i, item := range a.queue {
		if a.cfg.ParallelDownload && a.hasActiveSource(item.source) {
			continue
		}
		// Remove from queue.
		a.queue = append(a.queue[:i], a.queue[i+1:]...)
		return a.startDownload(item)
	}

	// Non-parallel mode: just take the front item.
	if !a.cfg.ParallelDownload {
		item := a.queue[0]
		a.queue = a.queue[1:]
		return a.startDownload(item)
	}

	return nil
}

func (a *App) startDownload(item queueItem) tea.Cmd {
	id := a.nextDownloadID
	a.nextDownloadID++

	ch := make(chan downloader.Progress, 64)
	p := progress.New(progress.WithDefaultGradient())
	if a.width > 0 {
		p.Width = a.width - 6
		if p.Width > 80 {
			p.Width = 80
		}
	}

	ad := &activeDownload{
		id:       id,
		item:     item,
		ch:       ch,
		progress: p,
	}
	a.active = append(a.active, ad)

	switch item.source {
	case sourceCivitai:
		client := a.client
		comfyPath := a.cfg.ComfyUIPath
		m := *item.civModel
		v := *item.civVersion
		var sf *api.ModelFile
		if item.civFile != nil {
			copy := *item.civFile
			sf = &copy
		}
		go func() {
			_, err := downloader.Download(client, &m, &v, comfyPath, ch, sf)
			if err != nil {
				ch <- downloader.Progress{Done: true, Err: err}
			}
		}()

	case sourceHuggingFace:
		client := a.hfClient
		comfyPath := a.cfg.ComfyUIPath
		m := *item.hfModel
		fn := item.hfFilename
		mt := item.hfType
		go func() {
			_, err := downloader.DownloadHF(client, &m, fn, mt, comfyPath, ch)
			if err != nil {
				ch <- downloader.Progress{Done: true, Err: err}
			}
		}()
	}

	return waitForProgressByID(id, ch)
}

func waitForProgressByID(id int, ch chan downloader.Progress) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return nil
		}
		if p.Done || p.Err != nil {
			return downloadCompleteMsg{downloadID: id, installed: p.Installed, err: p.Err}
		}
		return downloadProgressMsg{
			downloadID:      id,
			bytesDownloaded: p.BytesDownloaded,
			totalBytes:      p.TotalBytes,
		}
	}
}

func (a *App) findActive(id int) *activeDownload {
	for _, ad := range a.active {
		if ad.id == id {
			return ad
		}
	}
	return nil
}

func (a *App) removeActive(id int) {
	for i, ad := range a.active {
		if ad.id == id {
			a.active = append(a.active[:i], a.active[i+1:]...)
			return
		}
	}
}

func (a *App) hasActiveSource(src downloadSource) bool {
	for _, ad := range a.active {
		if ad.item.source == src {
			return true
		}
	}
	return false
}

// viewDownloadStatus renders the status bar section for active downloads and queue.
func (a *App) viewDownloadStatus() string {
	if len(a.active) == 0 && len(a.queue) == 0 {
		return ""
	}

	var b string
	for _, ad := range a.active {
		b += warningStyle.Render(fmt.Sprintf("  Downloading %s", ad.item.name)) + "\n"
		b += "  " + ad.progress.View() + "\n"
		if ad.total > 0 {
			b += mutedStyle.Render(fmt.Sprintf("  %s / %s (%.1f%%)",
				formatBytes(ad.bytes),
				formatBytes(ad.total),
				ad.pct*100)) + "\n"
		} else {
			b += mutedStyle.Render(fmt.Sprintf("  %s downloaded",
				formatBytes(ad.bytes))) + "\n"
		}
	}

	if len(a.queue) > 0 {
		b += mutedStyle.Render(fmt.Sprintf("  %d download(s) queued", len(a.queue))) + "\n"
	}

	return b
}
