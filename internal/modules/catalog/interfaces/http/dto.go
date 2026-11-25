package http

import (
	"time"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
	"github.com/saransh1220/blueprint-audio/internal/shared/money"
)

// SpecResponse is the PUBLIC response
type SpecResponse struct {
	ID                uuid.UUID                 `json:"id"`
	ProducerID        uuid.UUID                 `json:"producer_id"`
	ProducerName      string                    `json:"producer_name"`
	ProducerHandle    string                    `json:"producer_handle"`
	Title             string                    `json:"title"`
	Category          string                    `json:"category"`
	Type              string                    `json:"type"`
	BPM               int                       `json:"bpm"`
	Key               string                    `json:"key"`
	Description       string                    `json:"description"`
	ImageURL          string                    `json:"image_url"`
	PreviewURL        string                    `json:"preview_url"`
	Price             float64                   `json:"price"`
	PriceMoney        money.Money               `json:"price_money"`
	DisplayPriceMoney money.Money               `json:"display_price_money"`
	SelectedPrice     *money.SelectedPriceOffer `json:"selected_price,omitempty"`
	Promotion         *SpecPromotion            `json:"promotion,omitempty"`
	Duration          int                       `json:"duration"`
	FreeMp3Enabled    bool                      `json:"free_mp3_enabled"`
	CreatedAt         time.Time                 `json:"created_at"`
	UpdatedAt         time.Time                 `json:"updated_at"`
	Licenses          []LicenseResponse         `json:"licenses,omitempty"`
	Genres            []GenreResponse           `json:"genres,omitempty"`
	Tags              []string                  `json:"tags,omitempty"`
	Analytics         *SpecAnalytics            `json:"analytics,omitempty"`
	ShortCode         *string                   `json:"short_code,omitempty"`
	Slug              *string                   `json:"slug,omitempty"`
	Moods             []string                  `json:"moods,omitempty"`
	Instruments       []string                  `json:"instruments,omitempty"`
	WaveformPeaks     []int64                   `json:"waveform_peaks,omitempty"`
	ProcessingStatus  string                    `json:"processing_status"`
}

// SpecAnalytics contains publicly visible analytics
type SpecAnalytics struct {
	PlayCount          int  `json:"play_count"`
	FavoriteCount      int  `json:"favorite_count"`
	TotalDownloadCount int  `json:"total_download_count"`
	IsFavorited        bool `json:"is_favorited"`
}

type HomepageResponse struct {
	GeneratedAt     time.Time                          `json:"generated_at"`
	CacheTTLSeconds int                                `json:"cache_ttl_seconds"`
	Stats           HomepageStatsResponse              `json:"stats"`
	Sections        map[string]HomepageSectionResponse `json:"sections"`
}

type HomepageStatsResponse struct {
	TotalLiveBeats int `json:"total_live_beats"`
	NewReleases7D  int `json:"new_releases_7d"`
	TotalProducers int `json:"total_producers"`
}

type HomepageSectionResponse struct {
	Title  string                 `json:"title"`
	Source string                 `json:"source"`
	Period string                 `json:"period,omitempty"`
	Items  []HomepageItemResponse `json:"items"`
}

type HomepageItemResponse struct {
	Rank     int                        `json:"rank"`
	Score    *float64                   `json:"score,omitempty"`
	Movement string                     `json:"movement"`
	Metrics  *domain.BeatRankingMetrics `json:"metrics,omitempty"`
	Spec     SpecResponse               `json:"spec"`
}

// LicenseResponse for nested license data
type LicenseResponse struct {
	ID                uuid.UUID                 `json:"id"`
	SpecID            uuid.UUID                 `json:"spec_id"`
	Type              string                    `json:"type"`
	Name              string                    `json:"name"`
	Price             float64                   `json:"price"`
	PriceMoney        money.Money               `json:"price_money"`
	DisplayPriceMoney money.Money               `json:"display_price_money"`
	SelectedPrice     *money.SelectedPriceOffer `json:"selected_price,omitempty"`
	Discount          *money.CheckoutDiscount   `json:"discount,omitempty"`
	Prices            []money.StoredPrice       `json:"prices,omitempty"`
	Features          []string                  `json:"features"`
	FileTypes         []string                  `json:"file_types"`
	CreatedAt         time.Time                 `json:"created_at"`
	UpdatedAt         time.Time                 `json:"updated_at"`
}

// GenreResponse for nested genre data
type GenreResponse struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
}

func ToSpecResponse(spec *domain.Spec) *SpecResponse {
	return ToSpecResponseForCurrency(spec, money.CurrencyINR)
}

func ToSpecResponseForCurrency(spec *domain.Spec, displayCurrency string) *SpecResponse {
	natCurrency := spec.PriceCurrency
	if natCurrency == "" {
		natCurrency = money.CurrencyINR
	}

	response := &SpecResponse{
		ID:                spec.ID,
		ProducerID:        spec.ProducerID,
		ProducerName:      spec.ProducerName,
		ProducerHandle:    spec.ProducerHandle,
		Title:             spec.Title,
		Category:          string(spec.Category),
		Type:              spec.Type,
		BPM:               spec.BPM,
		Key:               spec.Key,
		Description:       spec.Description,
		ImageURL:          spec.ImageUrl,
		PreviewURL:        spec.PreviewUrl,
		Price:             spec.BasePrice,
		PriceMoney:        nativeDisplayMoney(spec.BasePrice, spec.PriceCurrency),
		SelectedPrice:     &money.SelectedPriceOffer{Currency: displayCurrency, Selection: money.SelectionUnavailable},
		DisplayPriceMoney: money.Money{Currency: displayCurrency},
		Duration:          spec.Duration,
		FreeMp3Enabled:    spec.FreeMp3Enabled,
		CreatedAt:         spec.CreatedAt,
		UpdatedAt:         spec.UpdatedAt,
		Tags:              spec.Tags,
		ShortCode:         spec.ShortCode,
		Slug:              spec.Slug,
		Moods:             spec.Moods,
		Instruments:       spec.Instruments,
		WaveformPeaks:     spec.WaveformPeaks,
		ProcessingStatus:  string(spec.ProcessingStatus),
	}

	// Convert licenses
	if len(spec.Licenses) > 0 {
		response.Licenses = make([]LicenseResponse, len(spec.Licenses))
		for i, license := range spec.Licenses {
			licNatCurr := license.PriceCurrency
			if licNatCurr == "" {
				licNatCurr = natCurrency
			}

			prices := license.Prices
			offer := money.SelectPriceOffer(prices, licNatCurr, displayCurrency)

			scale := 100
			if def, ok := money.LookupCurrency(licNatCurr); ok && def.MinorUnitScale > 0 {
				scale = def.MinorUnitScale
			}
			nativeMinor, err := money.MajorToMinorScale(license.Price, scale)
			if err != nil {
				nativeMinor = 0
			}
			priceMoney := money.Money{
				AmountMinor: nativeMinor,
				AmountMajor: money.MinorToMajor(nativeMinor, scale),
				Currency:    licNatCurr,
			}

			var displayPriceMoney money.Money
			if offer.IsPurchasable && offer.Selection != money.SelectionUnavailable {
				displayPriceMoney = money.Money{
					AmountMinor: offer.AmountMinor,
					AmountMajor: offer.AmountMajor,
					Currency:    offer.Currency,
				}
			} else {
				displayPriceMoney = money.Money{
					AmountMinor: 0,
					AmountMajor: 0,
					Currency:    displayCurrency,
				}
			}

			response.Licenses[i] = LicenseResponse{
				ID:                license.ID,
				SpecID:            license.SpecID,
				Type:              string(license.LicenseType),
				Name:              license.Name,
				Price:             license.Price, // Native scalar preserved for owner edits
				PriceMoney:        priceMoney,
				DisplayPriceMoney: displayPriceMoney,
				SelectedPrice:     &offer,
				Prices:            prices,
				Features:          license.Features,
				FileTypes:         license.FileTypes,
				CreatedAt:         license.CreatedAt,
				UpdatedAt:         license.UpdatedAt,
			}
		}

		// Starting price ("From $X"): derive minimum among valid purchasable offers.
		// Priority 1: exact matches to displayCurrency.
		// Priority 2: native fallbacks, only comparing amongst identical fallback currency.
		var lowestExact *money.SelectedPriceOffer
		var lowestFallback *money.SelectedPriceOffer

		for _, licResp := range response.Licenses {
			offer := licResp.SelectedPrice
			if offer == nil || !offer.IsPurchasable || offer.Selection == money.SelectionUnavailable {
				continue
			}
			if offer.Selection == money.SelectionExact && offer.Currency == displayCurrency {
				if lowestExact == nil || offer.AmountMinor < lowestExact.AmountMinor {
					lowestExact = offer
				}
			} else if offer.Selection == money.SelectionNativeFallback {
				if lowestFallback == nil || (offer.Currency == lowestFallback.Currency && offer.AmountMinor < lowestFallback.AmountMinor) {
					lowestFallback = offer
				}
			}
		}

		var startingOffer *money.SelectedPriceOffer
		if lowestExact != nil {
			startingOffer = lowestExact
		} else if lowestFallback != nil {
			startingOffer = lowestFallback
		}

		if startingOffer != nil {
			response.SelectedPrice = startingOffer
			response.DisplayPriceMoney = money.Money{
				AmountMinor: startingOffer.AmountMinor,
				AmountMajor: startingOffer.AmountMajor,
				Currency:    startingOffer.Currency,
			}
		} else {
			response.SelectedPrice = &money.SelectedPriceOffer{
				Currency:      displayCurrency,
				Selection:     money.SelectionUnavailable,
				IsPurchasable: false,
			}
			response.DisplayPriceMoney = money.Money{
				AmountMinor: 0,
				AmountMajor: 0,
				Currency:    displayCurrency,
			}
		}
	}

	// Convert genres
	if len(spec.Genres) > 0 {
		response.Genres = make([]GenreResponse, len(spec.Genres))
		for i, genre := range spec.Genres {
			response.Genres[i] = GenreResponse{
				ID:        genre.ID,
				Name:      genre.Name,
				Slug:      genre.Slug,
				CreatedAt: genre.CreatedAt,
			}
		}
	}

	return response
}

// Native compatibility values are never exchange-rate quotes.
func nativeDisplayMoney(amount float64, currency string) money.Money {
	if currency == "" {
		currency = money.CurrencyINR
	}
	scale := 100
	if def, ok := money.LookupCurrency(currency); ok && def.MinorUnitScale > 0 {
		scale = def.MinorUnitScale
	}
	minor, err := money.MajorToMinorScale(amount, scale)
	if err != nil {
		return money.Money{Currency: currency}
	}
	return money.Money{Currency: currency, AmountMinor: minor, AmountMajor: money.MinorToMajor(minor, scale)}
}
