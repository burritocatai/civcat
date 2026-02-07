package downloader

import (
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"time"

	"github.com/burritocatai/civcat/internal/api"
	"github.com/burritocatai/civcat/internal/tracker"
)

// ComfyUI subdirectory mapping for each model type.
var modelDirs = map[api.ModelType]string{
	api.ModelTypeCheckpoint:        "models/checkpoints",
	api.ModelTypeLORA:              "models/loras",
	api.ModelTypeTextualInversion:  "models/embeddings",
	api.ModelTypeHypernetwork:      "models/hypernetworks",
	api.ModelTypeAestheticGradient: "models/hypernetworks",
	api.ModelTypeControlnet:        "models/controlnet",
	api.ModelTypePoses:             "models/controlnet/poses",
	api.ModelTypeVAE:               "models/vae",
	api.ModelTypeUpscaler:          "models/upscale_models",
	api.ModelTypeMotionModule:      "models/animatediff_motion_lora",
	api.ModelTypeWildcards:         "models/wildcards",
	api.ModelTypeWorkflows:         "models/workflows",
	api.ModelTypeOther:             "models/other",
}

// SubdirForType returns the ComfyUI subdirectory for a model type.
func SubdirForType(t api.ModelType) string {
	if dir, ok := modelDirs[t]; ok {
		return dir
	}
	return "models/other"
}

// Progress is sent on the progress channel during download.
type Progress struct {
	BytesDownloaded int64
	TotalBytes      int64
	Done            bool
	Err             error
}

// Download downloads a model version and installs it to the proper ComfyUI directory.
// It returns the installed model info. Progress updates are sent on the progressCh if non-nil.
func Download(
	client *api.Client,
	model *api.Model,
	version *api.ModelVersion,
	comfyUIPath string,
	progressCh chan<- Progress,
) (*tracker.InstalledModel, error) {
	// Determine target directory.
	subdir := SubdirForType(model.Type)
	targetDir := filepath.Join(comfyUIPath, subdir)

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating target directory: %w", err)
	}

	// Find the primary file to get expected filename and hash.
	var primaryFile *api.ModelFile
	for i := range version.Files {
		if version.Files[i].Primary {
			primaryFile = &version.Files[i]
			break
		}
	}
	if primaryFile == nil && len(version.Files) > 0 {
		primaryFile = &version.Files[0]
	}

	// Start download.
	resp, err := client.DownloadFile(version.ID)
	if err != nil {
		return nil, fmt.Errorf("downloading model: %w", err)
	}
	defer resp.Body.Close()

	// Determine filename from Content-Disposition header, falling back to known name.
	fileName := ""
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		_, params, err := mime.ParseMediaType(cd)
		if err == nil {
			fileName = params["filename"]
		}
	}
	if fileName == "" && primaryFile != nil {
		fileName = primaryFile.Name
	}
	if fileName == "" {
		fileName = fmt.Sprintf("model_%d_%d.safetensors", model.ID, version.ID)
	}

	targetPath := filepath.Join(targetDir, fileName)

	// Write to temp file first, then rename.
	tmpFile, err := os.CreateTemp(targetDir, ".civcat-dl-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		os.Remove(tmpPath) // clean up on error
	}()

	totalBytes := resp.ContentLength
	var downloaded int64

	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := tmpFile.Write(buf[:n]); writeErr != nil {
				return nil, fmt.Errorf("writing file: %w", writeErr)
			}
			downloaded += int64(n)
			if progressCh != nil {
				progressCh <- Progress{
					BytesDownloaded: downloaded,
					TotalBytes:      totalBytes,
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return nil, fmt.Errorf("reading response: %w", readErr)
		}
	}

	tmpFile.Close()

	// Move temp file to final location.
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return nil, fmt.Errorf("moving file to destination: %w", err)
	}

	fileHash := ""
	sizeKB := float64(0)
	if primaryFile != nil {
		fileHash = primaryFile.Hashes.SHA256
		sizeKB = primaryFile.SizeKB
	}

	installed := &tracker.InstalledModel{
		ModelID:     model.ID,
		ModelName:   model.Name,
		VersionID:   version.ID,
		VersionName: version.Name,
		Type:        model.Type,
		BaseModel:   version.BaseModel,
		FileName:    fileName,
		FilePath:    targetPath,
		FileHash:    fileHash,
		SizeKB:      sizeKB,
		InstalledAt: time.Now(),
		Creator:     model.Creator.Username,
	}

	if progressCh != nil {
		progressCh <- Progress{
			BytesDownloaded: downloaded,
			TotalBytes:      totalBytes,
			Done:            true,
		}
	}

	return installed, nil
}
