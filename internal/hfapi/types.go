package hfapi

import "time"

// HFModel represents a model returned by the Hugging Face API.
type HFModel struct {
	ID            string    `json:"id"` // e.g. "stabilityai/stable-diffusion-xl-base-1.0"
	ModelID       string    `json:"modelId"`
	Author        string    `json:"author"`
	SHA           string    `json:"sha"`
	LastModified  time.Time `json:"lastModified"`
	Private       bool      `json:"private"`
	Disabled      bool      `json:"disabled"`
	Gated         interface{} `json:"gated"` // bool or string
	Downloads     int       `json:"downloads"`
	Likes         int       `json:"likes"`
	Tags          []string  `json:"tags"`
	PipelineTag   string    `json:"pipeline_tag"`
	LibraryName   string    `json:"library_name"`
	Siblings      []Sibling `json:"siblings"`
	CreatedAt     time.Time `json:"createdAt"`
}

// IsGated returns true if the model requires access approval.
func (m *HFModel) IsGated() bool {
	switch v := m.Gated.(type) {
	case bool:
		return v
	case string:
		return v != "" && v != "false"
	default:
		return false
	}
}

// Sibling represents a file in the model repository.
type Sibling struct {
	RFilename string `json:"rfilename"`
}

// IsDownloadable returns true if the file looks like a model weight file.
func (s *Sibling) IsDownloadable() bool {
	name := s.RFilename
	for _, ext := range downloadableExtensions {
		if len(name) > len(ext) && name[len(name)-len(ext):] == ext {
			return true
		}
	}
	return false
}

var downloadableExtensions = []string{
	".safetensors",
	".bin",
	".pt",
	".pth",
	".ckpt",
	".gguf",
	".onnx",
}
