package tracker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/burritocatai/civcat/internal/api"
	"github.com/burritocatai/civcat/internal/config"
)

const trackerFile = "models.json"

type InstalledModel struct {
	ModelID        int           `json:"model_id"`
	ModelName      string        `json:"model_name"`
	VersionID      int           `json:"version_id"`
	VersionName    string        `json:"version_name"`
	Type           api.ModelType `json:"type"`
	BaseModel      string        `json:"base_model"`
	FileName       string        `json:"file_name"`
	FilePath       string        `json:"file_path"`
	FileHash       string        `json:"file_hash"`
	SizeKB         float64       `json:"size_kb"`
	InstalledAt    time.Time     `json:"installed_at"`
	Creator        string        `json:"creator"`
	HasUpdate      bool          `json:"has_update,omitempty"`
	LatestVersion  int           `json:"latest_version,omitempty"`
}

type Tracker struct {
	mu     sync.RWMutex
	Models []InstalledModel `json:"models"`
	path   string
}

func New() (*Tracker, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return nil, err
	}

	t := &Tracker{
		path: filepath.Join(dir, trackerFile),
	}

	if err := t.load(); err != nil {
		return nil, err
	}

	return t, nil
}

func (t *Tracker) load() error {
	data, err := os.ReadFile(t.path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Models = []InstalledModel{}
			return nil
		}
		return fmt.Errorf("reading tracker: %w", err)
	}

	if err := json.Unmarshal(data, &t.Models); err != nil {
		return fmt.Errorf("parsing tracker: %w", err)
	}
	return nil
}

func (t *Tracker) Save() error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(t.path), 0o755); err != nil {
		return fmt.Errorf("creating tracker directory: %w", err)
	}

	data, err := json.MarshalIndent(t.Models, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling tracker: %w", err)
	}

	return os.WriteFile(t.path, data, 0o644)
}

func (t *Tracker) Add(m InstalledModel) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Replace if same model+version already tracked.
	for i, existing := range t.Models {
		if existing.ModelID == m.ModelID {
			t.Models[i] = m
			return t.saveUnlocked()
		}
	}

	t.Models = append(t.Models, m)
	return t.saveUnlocked()
}

func (t *Tracker) Remove(modelID int) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	for i, m := range t.Models {
		if m.ModelID == modelID {
			t.Models = append(t.Models[:i], t.Models[i+1:]...)
			return t.saveUnlocked()
		}
	}
	return nil
}

func (t *Tracker) GetAll() []InstalledModel {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]InstalledModel, len(t.Models))
	copy(result, t.Models)
	return result
}

func (t *Tracker) IsInstalled(modelID int) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, m := range t.Models {
		if m.ModelID == modelID {
			return true
		}
	}
	return false
}

func (t *Tracker) GetByModelID(modelID int) *InstalledModel {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, m := range t.Models {
		if m.ModelID == modelID {
			result := m
			return &result
		}
	}
	return nil
}

func (t *Tracker) MarkUpdate(modelID, latestVersionID int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i, m := range t.Models {
		if m.ModelID == modelID {
			t.Models[i].HasUpdate = true
			t.Models[i].LatestVersion = latestVersionID
			return t.saveUnlocked()
		}
	}
	return nil
}

func (t *Tracker) ClearUpdate(modelID int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i, m := range t.Models {
		if m.ModelID == modelID {
			t.Models[i].HasUpdate = false
			t.Models[i].LatestVersion = 0
			return t.saveUnlocked()
		}
	}
	return nil
}

func (t *Tracker) saveUnlocked() error {
	if err := os.MkdirAll(filepath.Dir(t.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(t.Models, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(t.path, data, 0o644)
}
