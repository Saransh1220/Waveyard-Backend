package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/saransh1220/blueprint-audio/internal/shared/money"
)

type Category string
type LicenseType string

const (
	CategoryBeat   Category = "beat"
	CategorySample Category = "sample"
)

const (
	LicenseBasic     LicenseType = "Basic"
	LicensePremium   LicenseType = "Premium"
	LicenseTrackout  LicenseType = "Trackout"
	LicenseUnlimited LicenseType = "Unlimited"
)

// Spec represents a beat or sample package
type Spec struct {
	ID             uuid.UUID      `json:"id" db:"id"`
	ProducerID     uuid.UUID      `json:"producer_id" db:"producer_id"`
	ProducerName   string         `json:"producer_name" db:"producer_name"`
	Title          string         `json:"title" db:"title"`
	Category       Category       `json:"category" db:"category"`
	Type           string         `json:"type" db:"type"` // e.g., WAV, STEMS, PACK
	BPM            int            `json:"bpm" db:"bpm"`
	Key            string         `json:"key" db:"key"`
	ImageUrl       string         `json:"image_url" db:"image_url"`
	PreviewUrl     string         `json:"preview_url" db:"preview_url"`
	WavUrl         *string        `json:"wav_url,omitempty" db:"wav_url"`
	StemsUrl       *string        `json:"stems_url,omitempty" db:"stems_url"`
	BasePrice      float64        `json:"price" db:"base_price"`
	PriceCurrency  string         `json:"price_currency" db:"price_currency"`
	Description    string         `json:"description" db:"description"`
	Duration       int            `json:"duration" db:"duration"`
	FreeMp3Enabled bool           `json:"free_mp3_enabled" db:"free_mp3_enabled"`
	CreatedAt      time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at" db:"updated_at"`
	DeletedAt      *time.Time     `json:"deleted_at,omitempty" db:"deleted_at"`
	IsDeleted      bool           `json:"is_deleted" db:"is_deleted"`
	Moods          pq.StringArray `json:"moods" db:"moods"`
	Instruments    pq.StringArray `json:"instruments" db:"instruments"`
	WaveformPeaks  pq.Int64Array  `json:"waveform_peaks" db:"waveform_peaks"`
	Slug           *string        `json:"slug" db:"slug"`
	ShortCode      *string        `json:"short_code" db:"short_code"`
	ProducerHandle string         `json:"producer_handle" db:"producer_handle"`

	// Processing Status
	ProcessingStatus ProcessingStatus `json:"processing_status" db:"processing_status"`

	// Relations
	Licenses []LicenseOption `json:"licenses,omitempty"`
	Genres   []Genre         `json:"genres,omitempty"`
	Tags     pq.StringArray  `json:"tags,omitempty" db:"tags"`

	SelectedPrice *money.SelectedPriceOffer `json:"selected_price,omitempty"`
}

type ProcessingStatus string

const (
	ProcessingStatusPending    ProcessingStatus = "pending"
	ProcessingStatusProcessing ProcessingStatus = "processing"
	ProcessingStatusCompleted  ProcessingStatus = "completed"
	ProcessingStatusFailed     ProcessingStatus = "failed"
)

// LicenseOption defines the pricing and features for a specific spec
type LicenseOption struct {
	ID            uuid.UUID                 `json:"id" db:"id"`
	SpecID        uuid.UUID                 `json:"spec_id" db:"spec_id"`
	LicenseType   LicenseType               `json:"type" db:"license_type"`
	Name          string                    `json:"name" db:"name"`
	Price         float64                   `json:"price" db:"price"`
	PriceCurrency string                    `json:"price_currency" db:"price_currency"`
	Features      pq.StringArray            `json:"features" db:"features"`
	FileTypes     pq.StringArray            `json:"file_types" db:"file_types"`
	CreatedAt     time.Time                 `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time                 `json:"updated_at" db:"updated_at"`
	IsDeleted     bool                      `json:"is_deleted" db:"is_deleted"`
	Prices        []money.StoredPrice       `json:"prices,omitempty"`
	SelectedPrice *money.SelectedPriceOffer `json:"selected_price,omitempty"`
}

// Genre represents a musical genre
type Genre struct {
	ID        uuid.UUID `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	Slug      string    `json:"slug" db:"slug"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// SpecFilter contains all possible filters for listing specs
type ProducerSpecFilter struct {
	BeatsOnly bool
	Search    string
	IDs       []uuid.UUID
}

type SpecFilter struct {
	Currency    string // Effective buyer currency for exact stored-price filtering and sorting.
	Category    Category
	Genres      []string
	Tags        []string
	Search      string
	MinBPM      int
	MaxBPM      int
	MinPrice    float64
	MaxPrice    float64
	Key         string
	Limit       int
	Offset      int
	Sort        string
	Page        int
	Moods       []string
	Instruments []string
	MinDuration int
	MaxDuration int
}

// SpecRepository defines the contract for spec data access
type SpecRepository interface {
	Create(ctx context.Context, spec *Spec) error
	GetByID(ctx context.Context, id uuid.UUID) (*Spec, error)
	GetByIDSystem(ctx context.Context, id uuid.UUID) (*Spec, error)
	List(ctx context.Context, filter SpecFilter) ([]Spec, int, error)
	Update(ctx context.Context, spec *Spec) error
	UpdateFilesAndStatus(ctx context.Context, id uuid.UUID, files map[string]*string, status ProcessingStatus) error
	Delete(ctx context.Context, id uuid.UUID, producerID uuid.UUID) error
	ListByUserID(ctx context.Context, producerID uuid.UUID, limit, offset int, filters ...ProducerSpecFilter) ([]Spec, int, error)
	GetBySlug(ctx context.Context, slug string) (*Spec, error)
	GetByShortCode(ctx context.Context, shortCode string) (*Spec, error)
	GetHomepageStats(ctx context.Context) (*HomepageStats, error)
	GetNewestBeats(ctx context.Context, limit int) ([]Spec, error)
	GetRankedSpecs(ctx context.Context, section, period string, limit int) ([]RankingRow, error)
	GetRankingFreshness(ctx context.Context, section, period string) (*RankingFreshness, error)
	RecalculateBeatRankings(ctx context.Context, section, period string) error
}

// SpecFinder provides spec lookup capabilities for other modules (Payment, Analytics)
type SpecFinder interface {
	FindByID(ctx context.Context, id uuid.UUID) (*Spec, error)
	// FindByIDIncludingDeleted retrieves a spec even if it's soft-deleted
	// Used for license downloads where users should retain access to purchased content
	FindByIDIncludingDeleted(ctx context.Context, id uuid.UUID) (*Spec, error)
	FindWithLicenses(ctx context.Context, id uuid.UUID) (*Spec, error)
	Exists(ctx context.Context, id uuid.UUID) (bool, error)
	GetLicenseByID(ctx context.Context, licenseID uuid.UUID) (*LicenseOption, error)
	GetLicensePrices(ctx context.Context, licenseID uuid.UUID) ([]money.StoredPrice, error)
}
