package promotion

import (
	"time"

	"github.com/jmoiron/sqlx"
	catalogpostgres "github.com/saransh1220/blueprint-audio/internal/modules/catalog/infrastructure/persistence/postgres"
	"github.com/saransh1220/blueprint-audio/internal/modules/promotion/application"
	"github.com/saransh1220/blueprint-audio/internal/modules/promotion/infrastructure/persistence/postgres"
	promotionhttp "github.com/saransh1220/blueprint-audio/internal/modules/promotion/interfaces/http"
)

func NewHandler(db *sqlx.DB) *promotionhttp.Handler {
	repository := postgres.NewRepository(db, catalogpostgres.LockPromotionOptions)
	return promotionhttp.NewHandler(application.NewService(repository, time.Now))
}

func NewCheckoutCouponReader(db *sqlx.DB) *postgres.CheckoutCouponReader {
	return postgres.NewCheckoutCouponReader(db)
}
