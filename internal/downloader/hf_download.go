package downloader

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/burritocatai/civcat/internal/api"
	"github.com/burritocatai/civcat/internal/hfapi"
	"github.com/burritocatai/civcat/internal/tracker"
)

// DownloadHF downloads a file from a Hugging Face model repo and installs it
// to the proper ComfyUI directory based on the given model type.
func DownloadHF(
	client *hfapi.Client,
	model *hfapi.HFModel,
	filename string,
	modelType api.ModelType,
	comfyUIPath string,
	progressCh chan<- Progress,
) (*tracker.InstalledModel, error) {
	// Determine target directory.
	subdir := SubdirForType(modelType)
	targetDir := filepath.Join(comfyUIPath, subdir)

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating target directory: %w", err)
	}

	// Start download.
	resp, err := client.DownloadFile(model.ID, filename)
	if err != nil {
		return nil, fmt.Errorf("downloading from HuggingFace: %w", err)
	}
	defer resp.Body.Close()

	// Use just the base filename (HF files can have subdirectory paths).
	baseName := filepath.Base(filename)
	targetPath := filepath.Join(targetDir, baseName)

	// Write to temp file first, then rename.
	tmpFile, err := os.CreateTemp(targetDir, ".civcat-hf-dl-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		os.Remove(tmpPath)
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

	if err := os.Rename(tmpPath, targetPath); err != nil {
		return nil, fmt.Errorf("moving file to destination: %w", err)
	}

	installed := &tracker.InstalledModel{
		ModelID:     0, // HF models use string IDs; we store 0 and use ModelName for identification
		ModelName:   model.ID,
		VersionID:   0,
		VersionName: model.SHA,
		Type:        modelType,
		BaseModel:   "HuggingFace",
		FileName:    baseName,
		FilePath:    targetPath,
		FileHash:    model.SHA,
		SizeKB:      float64(downloaded) / 1024.0,
		InstalledAt: time.Now(),
		Creator:     model.Author,
		Source:      "huggingface",
	}

	if progressCh != nil {
		progressCh <- Progress{
			BytesDownloaded: downloaded,
			TotalBytes:      totalBytes,
			Done:            true,
			Installed:       installed,
		}
	}

	return installed, nil
}
