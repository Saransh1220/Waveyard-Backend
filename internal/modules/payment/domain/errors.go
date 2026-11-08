package domain

import "errors"

var (
	ErrOrderNotFound              = errors.New("order not found")
	ErrPaymentNotFound            = errors.New("payment not found")
	ErrLicenseNotFound            = errors.New("license not found")
	ErrInvalidOrderStatus         = errors.New("invalid order status")
	ErrInvalidPaymentStatus       = errors.New("invalid payment status")
	ErrLicenseRevoked             = errors.New("license has been revoked")
	ErrLicenseInactive            = errors.New("license is inactive")
	ErrInvalidSignature           = errors.New("invalid signature")
	ErrTimestampToleranceExceeded = errors.New("timestamp tolerance exceeded")
	ErrMalformedPayload           = errors.New("malformed payload")
	ErrPaymentNotCaptured         = errors.New("payment not captured")
	ErrAmountMismatch             = errors.New("payment amount or currency mismatch")
	ErrProviderMismatch           = errors.New("provider mismatch")
	ErrOrderNotPayable            = errors.New("order is not payable")
	ErrProviderCompletesByWebhook = errors.New("provider completes by webhook")
	ErrMissingWebhookSecret       = errors.New("webhook secret is not configured")
	ErrMissingPaymentID           = errors.New("missing payment id")

	// RW-1-9 Idempotency, Retries and Refunds
	ErrIdempotencyConflict   = errors.New("idempotency key conflict: request parameters do not match original request")
	ErrOrderReservedInFlight = errors.New("checkout creation in flight")
	ErrRefundAlreadyExists   = errors.New("a refund operation already exists for this payment")
	ErrRefundNotEligible     = errors.New("order is not eligible for refund")
	ErrRefundAmountMismatch  = errors.New("refund amount exceeds captured payment")
	ErrRefundProviderFailed  = errors.New("provider rejected refund")
	ErrDistinctCaptureAnomaly = errors.New("distinct capture reported for already paid order")
)
