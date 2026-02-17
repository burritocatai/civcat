package webui

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/burritocatai/civcat/internal/api"
	"github.com/burritocatai/civcat/internal/config"
	"github.com/burritocatai/civcat/internal/downloader"
	"github.com/burritocatai/civcat/internal/hfapi"
	"github.com/burritocatai/civcat/internal/tracker"
)

//go:embed web
var embeddedFS embed.FS

// Server bundles the HTTP server, queue store, and SSE broker together.
type Server struct {
	cfg    *config.Config
	client *api.Client
	hfc    *hfapi.Client
	trk    *tracker.Tracker
	store  *Store
	broker *SSEBroker
	http   *http.Server
}

// NewServer constructs and registers all routes. Call ListenAndServe to start.
func NewServer(cfg *config.Config, client *api.Client, hfc *hfapi.Client, trk *tracker.Tracker, port int) (*Server, error) {
	store, err := NewStore()
	if err != nil {
		return nil, fmt.Errorf("initializing queue store: %w", err)
	}

	broker := NewSSEBroker()
	srv := &Server{
		cfg:    cfg,
		client: client,
		hfc:    hfc,
		trk:    trk,
		store:  store,
		broker: broker,
	}

	mux := http.NewServeMux()

	// Static frontend — serve index.html for all non-API paths.
	webFS, err := fs.Sub(embeddedFS, "web")
	if err != nil {
		return nil, fmt.Errorf("preparing embedded FS: %w", err)
	}
	fileServer := http.FileServer(http.FS(webFS))
	mux.Handle("GET /", fileServer)

	// REST API
	mux.HandleFunc("GET /api/models", srv.handleListModels)
	mux.HandleFunc("GET /api/models/{id}", srv.handleGetModel)
	mux.HandleFunc("GET /api/queue", srv.handleListQueue)
	mux.HandleFunc("POST /api/queue", srv.handleAddToQueue)
	mux.HandleFunc("DELETE /api/queue/{id}", srv.handleDeleteQueueItem)
	mux.Handle("GET /api/events", broker)

	srv.http = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	return srv, nil
}

// ListenAndServe starts the HTTP server and the queue-processing worker.
// It blocks until the server is stopped.
func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.http.Addr)
	if err != nil {
		return fmt.Errorf("binding port: %w", err)
	}

	go s.processQueue()

	return s.http.Serve(ln)
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

// ---------------------------------------------------------------------------
// Queue worker
// ---------------------------------------------------------------------------

// processQueue is a loop that picks pending items one at a time and downloads
// them using the existing downloader package.
func (s *Server) processQueue() {
	for {
		item := s.store.NextPending()
		if item == nil {
			time.Sleep(1 * time.Second)
			continue
		}
		s.downloadItem(item)
	}
}

func (s *Server) downloadItem(item *QueueItem) {
	if s.cfg.ComfyUIPath == "" {
		s.store.Update(item.ID, func(q *QueueItem) {
			q.Status = StatusFailed
			q.Error = "ComfyUI path not configured — run 'civcat config' first"
		})
		s.broker.Publish("queue", s.store.GetAll())
		return
	}

	progressCh := make(chan downloader.Progress, 64)

	// Forward progress updates to SSE clients.
	go func() {
		for p := range progressCh {
			if s.store.IsCancelled(item.ID) {
				continue
			}
			var pct float64
			if p.TotalBytes > 0 {
				pct = float64(p.BytesDownloaded) / float64(p.TotalBytes) * 100
			}
			s.store.Update(item.ID, func(q *QueueItem) {
				q.BytesDone = p.BytesDownloaded
				q.BytesTotal = p.TotalBytes
				q.Progress = pct
			})
			s.broker.Publish("queue", s.store.GetAll())
		}
	}()

	var installed *tracker.InstalledModel
	var dlErr error

	switch item.Source {
	case "civitai":
		installed, dlErr = s.downloadCivitAI(item, progressCh)
	case "huggingface":
		installed, dlErr = s.downloadHuggingFace(item, progressCh)
	default:
		dlErr = fmt.Errorf("unknown source %q", item.Source)
	}

	close(progressCh)

	if s.store.IsCancelled(item.ID) {
		s.store.Remove(item.ID)
		s.broker.Publish("queue", s.store.GetAll())
		return
	}

	if dlErr != nil {
		s.store.Update(item.ID, func(q *QueueItem) {
			q.Status = StatusFailed
			q.Error = dlErr.Error()
			q.Progress = 0
		})
		s.broker.Publish("queue", s.store.GetAll())
		return
	}

	if installed != nil {
		_ = s.trk.Add(*installed)
	}

	s.store.Update(item.ID, func(q *QueueItem) {
		q.Status = StatusComplete
		q.Progress = 100
	})
	s.broker.Publish("queue", s.store.GetAll())
	s.broker.Publish("models", s.modelList())
}

func (s *Server) downloadCivitAI(item *QueueItem, progressCh chan<- downloader.Progress) (*tracker.InstalledModel, error) {
	modelID := item.CivitaiModelID
	versionID := item.CivitaiVersionID

	model, err := s.client.GetModel(modelID)
	if err != nil {
		return nil, fmt.Errorf("fetching model: %w", err)
	}

	var version *api.ModelVersion
	for i := range model.Versions {
		if versionID == 0 || model.Versions[i].ID == versionID {
			version = &model.Versions[i]
			break
		}
	}
	if version == nil {
		return nil, fmt.Errorf("version %d not found", versionID)
	}
	if version.IsEarlyAccess() {
		return nil, fmt.Errorf("model is in early access (%d days remaining)", version.EarlyAccessDaysLeft())
	}

	return downloader.Download(s.client, model, version, s.cfg.ComfyUIPath, progressCh)
}

func (s *Server) downloadHuggingFace(item *QueueItem, progressCh chan<- downloader.Progress) (*tracker.InstalledModel, error) {
	repoID := item.HFRepoID
	filename := item.HFFilename

	model, err := s.hfc.GetModel(repoID)
	if err != nil {
		return nil, fmt.Errorf("fetching HuggingFace model: %w", err)
	}

	if filename == "" {
		for _, sib := range model.Siblings {
			if sib.IsDownloadable() {
				filename = sib.RFilename
				break
			}
		}
	}
	if filename == "" {
		return nil, fmt.Errorf("no downloadable file found in repo %s", repoID)
	}

	if model.IsGated() {
		return nil, fmt.Errorf("model is gated — HuggingFace token required")
	}

	return downloader.DownloadHF(s.hfc, model, filename, api.ModelTypeOther, s.cfg.ComfyUIPath, progressCh)
}

// ---------------------------------------------------------------------------
// URL parsing and metadata pre-fetch
// ---------------------------------------------------------------------------

var (
	civitaiModelRe   = regexp.MustCompile(`civitai\.com/models/(\d+)`)
	civitaiVersionRe = regexp.MustCompile(`[?&]modelVersionId=(\d+)`)
	civitaiDLRe      = regexp.MustCompile(`civitai\.com/api/download/models/(\d+)`)
	hfRepoRe         = regexp.MustCompile(`huggingface\.co/([^/]+/[^/?#\s]+)`)
	hfFileRe         = regexp.MustCompile(`/resolve/[^/]+/(.+)$`)
	hfBlobRe         = regexp.MustCompile(`/blob/[^/]+/(.+)$`)
)

func detectSource(rawURL string) string {
	switch {
	case strings.Contains(rawURL, "civitai.com"):
		return "civitai"
	case strings.Contains(rawURL, "huggingface.co"):
		return "huggingface"
	default:
		return ""
	}
}

// prefetchCivitAI fetches model metadata from the CivitAI API, populates item
// fields, and returns the resolved model and version IDs.
func (s *Server) prefetchCivitAI(item *QueueItem) (modelID, versionID int, err error) {
	rawURL := item.URL

	// Direct download URL: civitai.com/api/download/models/{versionID}
	if m := civitaiDLRe.FindStringSubmatch(rawURL); m != nil {
		vID, _ := strconv.Atoi(m[1])
		versionID = vID
		version, verErr := s.client.GetModelVersion(versionID)
		if verErr != nil {
			return 0, 0, fmt.Errorf("fetching version: %w", verErr)
		}
		modelID = version.ModelID
	} else {
		if m := civitaiModelRe.FindStringSubmatch(rawURL); m != nil {
			modelID, _ = strconv.Atoi(m[1])
		}
		if modelID == 0 {
			return 0, 0, fmt.Errorf("could not parse CivitAI model ID from URL")
		}
		if m := civitaiVersionRe.FindStringSubmatch(rawURL); m != nil {
			versionID, _ = strconv.Atoi(m[1])
		}
	}

	model, merr := s.client.GetModel(modelID)
	if merr != nil {
		return 0, 0, fmt.Errorf("fetching model metadata: %w", merr)
	}

	item.ModelName = model.Name
	item.ModelType = string(model.Type)

	var targetVersion *api.ModelVersion
	for i := range model.Versions {
		if versionID == 0 || model.Versions[i].ID == versionID {
			targetVersion = &model.Versions[i]
			break
		}
	}
	if targetVersion != nil {
		item.BaseModel = targetVersion.BaseModel
		for _, img := range targetVersion.Images {
			item.PreviewURL = img.URL
			break
		}
		if versionID == 0 {
			versionID = targetVersion.ID
		}
	}

	return modelID, versionID, nil
}

// prefetchHuggingFace fetches model metadata from the HF API and populates item fields.
func (s *Server) prefetchHuggingFace(item *QueueItem) (repoID, filename string, err error) {
	rawURL := item.URL

	m := hfRepoRe.FindStringSubmatch(rawURL)
	if m == nil {
		return "", "", fmt.Errorf("could not parse HuggingFace repo ID from URL")
	}
	repoID = m[1]

	if fm := hfFileRe.FindStringSubmatch(rawURL); fm != nil {
		filename = fm[1]
	} else if fm := hfBlobRe.FindStringSubmatch(rawURL); fm != nil {
		filename = fm[1]
	}

	model, herr := s.hfc.GetModel(repoID)
	if herr != nil {
		return "", "", fmt.Errorf("fetching HF model metadata: %w", herr)
	}

	item.ModelName = model.ID
	item.BaseModel = "HuggingFace"

	if filename == "" {
		for _, sib := range model.Siblings {
			if sib.IsDownloadable() {
				filename = sib.RFilename
				break
			}
		}
	}

	return repoID, filename, nil
}

// ---------------------------------------------------------------------------
// REST API handlers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// modelIDFor produces a stable hex identifier for a model from its file path.
func modelIDFor(m tracker.InstalledModel) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(m.FilePath))
	return fmt.Sprintf("%08x", h.Sum32())
}

// ModelResponse wraps InstalledModel with an API-level id field.
type ModelResponse struct {
	ID string `json:"id"`
	tracker.InstalledModel
}

func (s *Server) modelList() []ModelResponse {
	models := s.trk.GetAll()
	out := make([]ModelResponse, len(models))
	for i, m := range models {
		out[i] = ModelResponse{ID: modelIDFor(m), InstalledModel: m}
	}
	return out
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.modelList())
}

func (s *Server) handleGetModel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	for _, m := range s.trk.GetAll() {
		if modelIDFor(m) == id {
			writeJSON(w, http.StatusOK, ModelResponse{ID: id, InstalledModel: m})
			return
		}
	}
	writeError(w, http.StatusNotFound, "model not found")
}

func (s *Server) handleListQueue(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.GetAll())
}

type addQueueRequest struct {
	URL string `json:"url"`
}

func (s *Server) handleAddToQueue(w http.ResponseWriter, r *http.Request) {
	var req addQueueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	if _, err := url.ParseRequestURI(req.URL); err != nil {
		writeError(w, http.StatusBadRequest, "invalid url")
		return
	}

	source := detectSource(req.URL)
	if source == "" {
		writeError(w, http.StatusBadRequest, "unsupported URL — must be a civitai.com or huggingface.co URL")
		return
	}

	item := s.store.Enqueue(req.URL, source)

	// Fetch metadata synchronously before returning so the client immediately
	// gets the model name, type, and preview image. The API clients have their
	// own 30s timeout so we don't need an additional context timeout here.
	switch source {
	case "civitai":
		mID, vID, err := s.prefetchCivitAI(item)
		if err == nil {
			s.store.Update(item.ID, func(q *QueueItem) {
				q.ModelName = item.ModelName
				q.ModelType = item.ModelType
				q.BaseModel = item.BaseModel
				q.PreviewURL = item.PreviewURL
				q.CivitaiModelID = mID
				q.CivitaiVersionID = vID
			})
		}
	case "huggingface":
		repoID, filename, err := s.prefetchHuggingFace(item)
		if err == nil {
			s.store.Update(item.ID, func(q *QueueItem) {
				q.ModelName = item.ModelName
				q.BaseModel = item.BaseModel
				q.HFRepoID = repoID
				q.HFFilename = filename
			})
		}
	}

	s.broker.Publish("queue", s.store.GetAll())
	writeJSON(w, http.StatusCreated, s.store.GetByID(item.ID))
}

func (s *Server) handleDeleteQueueItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.store.Remove(id) {
		writeError(w, http.StatusNotFound, "queue item not found")
		return
	}
	s.broker.Publish("queue", s.store.GetAll())
	w.WriteHeader(http.StatusNoContent)
}
