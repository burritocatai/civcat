package api

import "time"

// Model types returned by the Civitai API.
type ModelType string

const (
	ModelTypeCheckpoint        ModelType = "Checkpoint"
	ModelTypeLORA              ModelType = "LORA"
	ModelTypeTextualInversion  ModelType = "TextualInversion"
	ModelTypeHypernetwork      ModelType = "Hypernetwork"
	ModelTypeAestheticGradient ModelType = "AestheticGradient"
	ModelTypeControlnet        ModelType = "Controlnet"
	ModelTypePoses             ModelType = "Poses"
	ModelTypeVAE               ModelType = "VAE"
	ModelTypeUpscaler          ModelType = "Upscaler"
	ModelTypeMotionModule      ModelType = "MotionModule"
	ModelTypeWildcards         ModelType = "Wildcards"
	ModelTypeWorkflows         ModelType = "Workflows"
	ModelTypeTextEncoders      ModelType = "TextEncoders"
	ModelTypeDiffusionModels   ModelType = "DiffusionModels"
	ModelTypeClipVision        ModelType = "ClipVision"
	ModelTypeStyleModels       ModelType = "StyleModels"
	ModelTypeDiffusers         ModelType = "Diffusers"
	ModelTypeVAEApprox         ModelType = "VAEApprox"
	ModelTypeGligen            ModelType = "Gligen"
	ModelTypeLatentUpscale     ModelType = "LatentUpscale"
	ModelTypePhotomaker        ModelType = "Photomaker"
	ModelTypeClassifiers       ModelType = "Classifiers"
	ModelTypeModelPatches      ModelType = "ModelPatches"
	ModelTypeAudioEncoders     ModelType = "AudioEncoders"
	ModelTypeCLIP              ModelType = "CLIP"
	ModelTypeSAMs              ModelType = "SAMs"
	ModelTypeUNet              ModelType = "UNet"
	ModelTypeUltralyticsBbox   ModelType = "UltralyticsBbox"
	ModelTypeUltralyticsSegm   ModelType = "UltralyticsSegm"
	ModelTypeOther             ModelType = "Other"
)

type ModelsResponse struct {
	Items    []Model          `json:"items"`
	Metadata ResponseMetadata `json:"metadata"`
}

type ResponseMetadata struct {
	TotalItems  int    `json:"totalItems"`
	CurrentPage int    `json:"currentPage"`
	PageSize    int    `json:"pageSize"`
	TotalPages  int    `json:"totalPages"`
	NextPage    string `json:"nextPage"`
	PrevPage    string `json:"prevPage"`
	NextCursor  string `json:"nextCursor"`
}

type Model struct {
	ID          int            `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Type        ModelType      `json:"type"`
	NSFW        bool           `json:"nsfw"`
	Tags        []string       `json:"tags"`
	Mode        *string        `json:"mode"`
	Creator     Creator        `json:"creator"`
	Stats       ModelStats     `json:"stats"`
	Versions    []ModelVersion `json:"modelVersions"`
}

type Creator struct {
	Username string `json:"username"`
	Image    string `json:"image"`
}

type ModelStats struct {
	DownloadCount int     `json:"downloadCount"`
	FavoriteCount int     `json:"favoriteCount"`
	CommentCount  int     `json:"commentCount"`
	RatingCount   int     `json:"ratingCount"`
	Rating        float64 `json:"rating"`
}

type ModelVersion struct {
	ID                  int                `json:"id"`
	ModelID             int                `json:"modelId"`
	Name                string             `json:"name"`
	Description         string             `json:"description"`
	CreatedAt           time.Time          `json:"createdAt"`
	UpdatedAt           time.Time          `json:"updatedAt"`
	PublishedAt         *time.Time         `json:"publishedAt"`
	DownloadURL         string             `json:"downloadUrl"`
	TrainedWords        []string           `json:"trainedWords"`
	BaseModel           string             `json:"baseModel"`
	EarlyAccessTimeFrame int               `json:"earlyAccessTimeFrame"`
	EarlyAccessEndsAt    *time.Time        `json:"earlyAccessEndsAt"`
	Availability         string            `json:"availability"`
	Files                []ModelFile       `json:"files"`
	Images               []ModelImage      `json:"images"`
	Stats                ModelVersionStats `json:"stats"`
}

// IsEarlyAccess returns true if the version is currently in early access.
func (v *ModelVersion) IsEarlyAccess() bool {
	// Check explicit availability field first (newer API responses).
	if v.Availability == "EarlyAccess" {
		return true
	}
	// Check explicit end date.
	if v.EarlyAccessEndsAt != nil {
		return time.Now().Before(*v.EarlyAccessEndsAt)
	}
	// Fall back to computing from publishedAt + timeframe.
	if v.EarlyAccessTimeFrame > 0 && v.PublishedAt != nil {
		end := v.PublishedAt.Add(time.Duration(v.EarlyAccessTimeFrame) * 24 * time.Hour)
		return time.Now().Before(end)
	}
	return false
}

// EarlyAccessDaysLeft returns how many days remain, or 0 if not in early access.
func (v *ModelVersion) EarlyAccessDaysLeft() int {
	var end time.Time
	if v.EarlyAccessEndsAt != nil {
		end = *v.EarlyAccessEndsAt
	} else if v.EarlyAccessTimeFrame > 0 && v.PublishedAt != nil {
		end = v.PublishedAt.Add(time.Duration(v.EarlyAccessTimeFrame) * 24 * time.Hour)
	} else {
		return 0
	}
	days := int(time.Until(end).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days + 1
}

type ModelFile struct {
	ID                int          `json:"id"`
	Name              string       `json:"name"`
	SizeKB            float64      `json:"sizeKB"`
	Type              string       `json:"type"`
	Metadata          FileMetadata `json:"metadata"`
	PickleScanResult  string       `json:"pickleScanResult"`
	VirusScanResult   string       `json:"virusScanResult"`
	ScannedAt         *time.Time   `json:"scannedAt"`
	Hashes            FileHashes   `json:"hashes"`
	DownloadURL       string       `json:"downloadUrl"`
	Primary           bool         `json:"primary"`
}

type FileMetadata struct {
	FP     string `json:"fp"`
	Size   string `json:"size"`
	Format string `json:"format"`
}

type FileHashes struct {
	AutoV2 string `json:"AutoV2"`
	SHA256 string `json:"SHA256"`
	CRC32  string `json:"CRC32"`
	BLAKE3 string `json:"BLAKE3"`
}

type ModelImage struct {
	URL    string      `json:"url"`
	NSFW   interface{} `json:"nsfw"`
	Width  int         `json:"width"`
	Height int         `json:"height"`
	Hash   string      `json:"hash"`
	Meta   interface{} `json:"meta"`
}

type ModelVersionStats struct {
	DownloadCount int     `json:"downloadCount"`
	RatingCount   int     `json:"ratingCount"`
	Rating        float64 `json:"rating"`
}
