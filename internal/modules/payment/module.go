package payment

import (
	"github.com/jmoiron/sqlx"
	authDomain "github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	catalogDomain "github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/application"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/infrastructure/dodo"
	persistence "github.com/saransh1220/blueprint-audio/internal/modules/payment/infrastructure/persistence/postgres"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/infrastructure/razorpay"
	paymentHttp "github.com/saransh1220/blueprint-audio/internal/modules/payment/interfaces/http"
	sharedemail "github.com/saransh1220/blueprint-audio/internal/shared/infrastructure/email"
	sharedmoney "github.com/saransh1220/blueprint-audio/internal/shared/money"
)

// GatewayConfig holds the credentials for every payment gateway.
type GatewayConfig struct {
	Razorpay razorpay.Config
	Dodo     dodo.Config
}

// Module represents the Payment module
type Module struct {
	service  application.PaymentService
	handler  *paymentHttp.PaymentHandler
	gateways application.GatewayResolver
}

// NewModule creates and initializes the Payment module
func NewModule(
	db *sqlx.DB,
	specFinder catalogDomain.SpecFinder,
	userFinder authDomain.UserFinder,
	fileService application.FileService,
	emailSender sharedemail.Sender,
	appBaseURL string,
	gateways GatewayConfig,
) *Module {
	orderRepo := persistence.NewOrderRepository(db)
	paymentRepo := persistence.NewPaymentRepository(db)
	licenseRepo := persistence.NewLicenseRepository(db)
	idempotencyRepo := persistence.NewIdempotencyRepository(db)
	webhookRepo := persistence.NewWebhookRepository(db)
	refundRepo := persistence.NewRefundRepository(db)
	txRunner := persistence.NewTxRunner(db)

	if gateways.Dodo.ReturnURL == "" {
		gateways.Dodo.ReturnURL = appBaseURL + "/payment/return"
	}
	// Which gateway charges which currency is decided here, in one place.
	registry, err := application.NewGatewayRegistry(
		map[string]string{
			sharedmoney.CurrencyINR: domain.ProviderRazorpay,
			sharedmoney.CurrencyUSD: domain.ProviderDodo,
		},
		razorpay.NewGateway(gateways.Razorpay),
		dodo.NewGateway(gateways.Dodo),
	)
	if err != nil {
		panic(err) // a wiring mistake, caught at startup
	}

	service := application.NewPaymentService(orderRepo, paymentRepo, licenseRepo, specFinder, userFinder, fileService, emailSender, appBaseURL, registry, txRunner)
	if s, ok := service.(interface {
		SetIdempotencyRepository(domain.IdempotencyRepository)
		SetWebhookRepository(domain.WebhookEventRepository)
		SetRefundRepository(domain.RefundRepository)
	}); ok {
		s.SetIdempotencyRepository(idempotencyRepo)
		s.SetWebhookRepository(webhookRepo)
		s.SetRefundRepository(refundRepo)
	}

	handler := paymentHttp.NewPaymentHandler(service)

	return &Module{
		service:  service,
		handler:  handler,
		gateways: registry,
	}
}

// HTTPHandler returns the HTTP handler
func (m *Module) HTTPHandler() *paymentHttp.PaymentHandler {
	return m.handler
}

func (m *Module) Service() application.PaymentService {
	return m.service
}

func (m *Module) SetSnapshotter(snapshotter domain.ReportingRateSnapshotter) {
	if s, ok := m.service.(interface{ SetReportingRateSnapshotter(domain.ReportingRateSnapshotter) }); ok {
		s.SetReportingRateSnapshotter(snapshotter)
	}
}

// CurrencyGatewayNames exposes the registry's actual routing without provider secrets.
func (m *Module) CurrencyGatewayNames() map[string]string {
	names := make(map[string]string)
	for _, currency := range sharedmoney.SupportedCurrencies() {
		gateway, err := m.gateways.ForCurrency(currency.Code)
		if err == nil {
			names[currency.Code] = gateway.Name()
		}
	}
	return names
}
