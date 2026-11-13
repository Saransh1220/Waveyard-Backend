CREATE TABLE payment_provider_refunds (
    provider TEXT NOT NULL,
    provider_refund_id TEXT NOT NULL,
    payment_id UUID NOT NULL REFERENCES payments(id) ON DELETE RESTRICT,
    amount INTEGER NOT NULL CHECK (amount > 0),
    status TEXT NOT NULL CHECK (status IN ('pending', 'succeeded', 'failed')),
    PRIMARY KEY (provider, provider_refund_id)
);

CREATE INDEX idx_payment_provider_refunds_payment ON payment_provider_refunds(payment_id);
