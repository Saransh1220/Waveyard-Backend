package http

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/catalog/application"
	"github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
	filestorageDomain "github.com/saransh1220/blueprint-audio/internal/modules/filestorage/domain"
	"github.com/stretchr/testify/assert"
)

func TestUploadDTO_Conversions(t *testing.T) {
	t.Run("CreateSpecMetadataRequest.toSpec", func(t *testing.T) {
		activeFalse := false
		req := CreateSpecMetadataRequest{
			Title:          "Beat 1",
			Category:       domain.CategoryBeat,
			Type:           "trap",
			BPM:            140,
			Key:            "Am",
			Price:          29.99,
			PriceCurrency:  "USD",
			Description:    "Hot beat",
			FreeMP3Enabled: true,
			Tags:           []string{"trap", "dark"},
			Genres: []CreateGenreRequest{
				{Name: "Hip Hop", Slug: "hip-hop"},
			},
			Licenses: []CreateLicenseRequest{
				{
					Type:          domain.LicenseType("Basic"),
					Name:          "Basic Lease",
					Price:         29.99,
					PriceCurrency: "USD",
					Prices: []PriceInputRequest{
						{Currency: "USD", AmountMinor: 2999},
						{Currency: "INR", AmountMinor: 249900, IsActive: &activeFalse},
					},
				},
			},
		}

		spec := req.toSpec()
		assert.Equal(t, "Beat 1", spec.Title)
		assert.Len(t, spec.Licenses, 1)
		assert.Len(t, spec.Licenses[0].Prices, 2)
		assert.True(t, spec.Licenses[0].Prices[0].IsActive)
		assert.False(t, spec.Licenses[0].Prices[1].IsActive)
	})

	t.Run("CreateUploadFileRequest.toCommand", func(t *testing.T) {
		req := CreateUploadFileRequest{
			Kind:        domain.UploadAssetWAV,
			FileName:    "beat.wav",
			ContentType: "audio/wav",
			SizeBytes:   1024,
		}
		cmd := req.toCommand()
		assert.Equal(t, domain.UploadAssetWAV, cmd.Kind)
		assert.Equal(t, "beat.wav", cmd.FileName)
	})

	t.Run("toCreateUploadResponse and toPresignedUploadResponse", func(t *testing.T) {
		upID := uuid.New()
		spID := uuid.New()
		now := time.Now()
		cResp := toCreateUploadResponse(&application.InitiateSpecUploadResult{
			UploadID:  upID,
			SpecID:    spID,
			ExpiresAt: now,
		})
		assert.Equal(t, upID, cResp.UploadID)

		pResp := toPresignedUploadResponse(&application.UploadInstruction{
			AssetID:   upID,
			Kind:      domain.UploadAssetWAV,
			ObjectKey: "key/1",
			Upload: filestorageDomain.PresignedUpload{
				URL:     "https://s3.example.com",
				Method:  "PUT",
				Headers: map[string]string{"h": "v"},
			},
		})
		assert.Equal(t, upID, pResp.AssetID)
		assert.Equal(t, "https://s3.example.com", pResp.UploadURL)
	})

	t.Run("toConfirmedUploadFileResponse and toUploadStatusResponse", func(t *testing.T) {
		assetID := uuid.New()
		actualType := "audio/x-wav"
		actualSize := int64(2048)
		asset := &domain.SpecUploadAsset{
			ID:                  assetID,
			Kind:                domain.UploadAssetWAV,
			FileName:            "track.wav",
			DeclaredContentType: "audio/wav",
			ActualContentType:   &actualType,
			ExpectedSize:        1024,
			ActualSize:          &actualSize,
		}
		cResp := toConfirmedUploadFileResponse(asset)
		assert.Equal(t, "audio/x-wav", cResp.ContentType)
		assert.Equal(t, int64(2048), cResp.SizeBytes)

		errMsg := "failed"
		stResp := toUploadStatusResponse(&domain.SpecUploadStatus{
			UploadID:         assetID,
			Status:           domain.UploadStatusCompleted,
			ProcessingStatus: domain.ProcessingStatusCompleted,
			ErrorMessage:     &errMsg,
		})
		assert.Equal(t, domain.UploadStatusCompleted, stResp.Status)
		assert.Equal(t, &errMsg, stResp.Error)
	})
}
