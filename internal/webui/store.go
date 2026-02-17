package webui

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/burritocatai/civcat/internal/config"
)

// QueueStatus represents the state of a download queue item.
type QueueStatus string

const (
	StatusPending     QueueStatus = "pending"
	StatusDownloading QueueStatus = "downloading"
	StatusComplete    QueueStatus = "complete"
	StatusFailed      QueueStatus = "failed"
	StatusCancelled   QueueStatus = "cancelled"
)

// QueueItem represents a single entry in the download queue.
type QueueItem struct {
	ID         string      `json:"id"`
	URL        string      `json:"url"`
	Status     QueueStatus `json:"status"`
	Progress   float64     `json:"progress"`   // 0–100
	BytesDone  int64       `json:"bytes_done"`
	BytesTotal int64       `json:"bytes_total"`
	Error      string      `json:"error,omitempty"`
	AddedAt    time.Time   `json:"added_at"`

	// Metadata fetched from the source API before downloading.
	ModelName  string `json:"model_name,omitempty"`
	ModelType  string `json:"model_type,omitempty"`
	BaseModel  string `json:"base_model,omitempty"`
	PreviewURL string `json:"preview_url,omitempty"`
	Source     string `json:"source"` // "civitai" or "huggingface"

	// Resolved routing info set during pre-fetch; persisted so restarts
	// can resume pending downloads without re-fetching metadata.
	CivitaiModelID   int    `json:"civitai_model_id,omitempty"`
	CivitaiVersionID int    `json:"civitai_version_id,omitempty"`
	HFRepoID         string `json:"hf_repo_id,omitempty"`
	HFFilename       string `json:"hf_filename,omitempty"`
}

// Store is a thread-safe in-memory queue backed by a JSON file on disk.
type Store struct {
	mu    sync.RWMutex
	items []*QueueItem
	path  string
}

// NewStore creates or loads the queue store from disk.
func NewStore() (*Store, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return nil, err
	}
	s := &Store{
		path: filepath.Join(dir, "queue.json"),
	}
	s.load()

	// Reset any items that were mid-download when the process last died.
	for _, item := range s.items {
		if item.Status == StatusDownloading {
			item.Status = StatusPending
			item.Progress = 0
			item.BytesDone = 0
		}
	}
	return s, nil
}

func (s *Store) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		s.items = []*QueueItem{}
		return
	}
	if err := json.Unmarshal(data, &s.items); err != nil {
		s.items = []*QueueItem{}
	}
	if s.items == nil {
		s.items = []*QueueItem{}
	}
}

func (s *Store) save() {
	data, err := json.MarshalIndent(s.items, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(s.path), 0o755)
	_ = os.WriteFile(s.path, data, 0o644)
}

func newItemID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Enqueue adds a new item to the queue and persists it.
func (s *Store) Enqueue(url, source string) *QueueItem {
	s.mu.Lock()
	defer s.mu.Unlock()

	item := &QueueItem{
		ID:      newItemID(),
		URL:     url,
		Status:  StatusPending,
		Source:  source,
		AddedAt: time.Now(),
	}
	s.items = append(s.items, item)
	s.save()
	return item
}

// GetAll returns a snapshot of all queue items.
func (s *Store) GetAll() []*QueueItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*QueueItem, len(s.items))
	copy(out, s.items)
	return out
}

// GetByID returns the item with the given ID, or nil.
func (s *Store) GetByID(id string) *QueueItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.items {
		if item.ID == id {
			return item
		}
	}
	return nil
}

// Update calls fn on the item with the given ID while holding the write lock.
// Changes are persisted to disk.
func (s *Store) Update(id string, fn func(*QueueItem)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.items {
		if item.ID == id {
			fn(item)
			s.save()
			return
		}
	}
}

// Remove deletes an item from the queue. Returns false if not found.
// Items that are currently downloading are marked cancelled instead of deleted.
func (s *Store) Remove(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.items {
		if item.ID == id {
			if item.Status == StatusDownloading {
				item.Status = StatusCancelled
			} else {
				s.items = append(s.items[:i], s.items[i+1:]...)
			}
			s.save()
			return true
		}
	}
	return false
}

// NextPending atomically claims the next pending item for downloading.
// Returns nil if there is nothing waiting.
func (s *Store) NextPending() *QueueItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.items {
		if item.Status == StatusPending {
			item.Status = StatusDownloading
			s.save()
			return item
		}
	}
	return nil
}

// IsCancelled returns true if the item has been marked for cancellation.
func (s *Store) IsCancelled(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.items {
		if item.ID == id {
			return item.Status == StatusCancelled
		}
	}
	return true // not found → treat as cancelled
}
