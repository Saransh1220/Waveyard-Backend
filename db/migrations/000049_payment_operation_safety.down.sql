DROP INDEX uq_order_refunds_active_payment;

ALTER TABLE payment_webhook_events
    ADD CONSTRAINT payment_webhook_events_order_id_fkey
    FOREIGN KEY (order_id) REFERENCES orders(id) ON DELETE SET NULL NOT VALID;
