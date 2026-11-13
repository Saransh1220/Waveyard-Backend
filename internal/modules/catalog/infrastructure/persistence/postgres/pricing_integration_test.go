package postgres_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/saransh1220/blueprint-audio/internal/modules/catalog/application"
	"github.com/saransh1220/blueprint-audio/internal/modules/catalog/application/priceaudit"
	"github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
	pg "github.com/saransh1220/blueprint-audio/internal/modules/catalog/infrastructure/persistence/postgres"
	"github.com/saransh1220/blueprint-audio/internal/shared/infrastructure/config"
	"github.com/saransh1220/blueprint-audio/internal/shared/infrastructure/database"
	"github.com/saransh1220/blueprint-audio/internal/shared/money"
	"github.com/stretchr/testify/require"
)

// Clone only table definitions into a unique schema. Never apply migrations or
// mutate monetary records in the pre-existing shared test schema.
func isolatedPricingDB(t *testing.T) *sqlx.DB {
	t.Helper()
	cfg := config.Load()
	cfg.Database.Host = getEnvOrDefault("TEST_DB_HOST", "localhost")
	cfg.Database.Port = getEnvOrDefault("TEST_DB_PORT", "5433")
	cfg.Database.User = getEnvOrDefault("TEST_DB_USER", "postgres")
	cfg.Database.Password = getEnvOrDefault("TEST_DB_PASSWORD", "postgres")
	cfg.Database.DBName = getEnvOrDefault("TEST_DB_NAME", "blueprint-audio")
	cfg.Database.SSLMode = getEnvOrDefault("TEST_DB_SSLMODE", "disable")
	db, err := database.NewPostgresDB(cfg.Database)
	if err != nil {
		t.Skipf("Postgres integration environment unavailable: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	schema := "pricing_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, func() error { _, e := db.Exec(`CREATE SCHEMA ` + pq.QuoteIdentifier(schema)); return e }())
	t.Cleanup(func() {
		_, _ = db.Exec(`SET search_path TO public`)
		_, e := db.Exec(`DROP SCHEMA ` + pq.QuoteIdentifier(schema) + ` CASCADE`)
		if e != nil {
			t.Errorf("cleanup isolated schema: %v", e)
		}
		db.Close()
	})
	for _, table := range []string{"users", "specs", "spec_analytics", "genres", "spec_genres", "license_options", "license_option_prices", "licenses"} {
		_, err = db.Exec(`CREATE TABLE ` + pq.QuoteIdentifier(schema) + `.` + pq.QuoteIdentifier(table) + ` (LIKE public.` + pq.QuoteIdentifier(table) + ` INCLUDING ALL)`)
		require.NoError(t, err)
	}
	_, err = db.Exec(`SET search_path TO ` + pq.QuoteIdentifier(schema) + `,public`)
	require.NoError(t, err)
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Dir(file)
	for {
		if _, e := os.Stat(filepath.Join(root, "go.mod")); e == nil {
			break
		}
		parent := filepath.Dir(root)
		require.NotEqual(t, root, parent)
		root = parent
	}
	for _, name := range []string{"000042_add_price_revisions_and_order_snapshots.up.sql", "000043_add_price_reconciliation_batches.up.sql"} {
		body, e := os.ReadFile(filepath.Join(root, "db", "migrations", name))
		require.NoError(t, e)
		_, e = db.Exec(string(body))
		require.NoError(t, e)
	}
	return db
}

func TestPricingIntegrationEditReloadListAndManifestRecovery(t *testing.T) {
	db := isolatedPricingDB(t)
	ctx := context.Background()
	repo := pg.NewSpecRepository(db)
	owner := uuid.New()
	name := uuid.NewString()
	_, err := db.Exec(`INSERT INTO users(id,email,password_hash,name,display_name,handle,role) VALUES($1,$2,'hash',$3,$4,$5,'producer')`, owner, name+"@example.test", name, name, "p"+strings.ReplaceAll(name, "-", "")[:12])
	require.NoError(t, err)
	spec := &domain.Spec{ProducerID: owner, Title: "Isolated pricing", BPM: 120, Key: "C MAJOR", Category: domain.CategorySample, Type: "pack", PriceCurrency: "INR", BasePrice: 1000, ProcessingStatus: domain.ProcessingStatusCompleted,
		Licenses: []domain.LicenseOption{{LicenseType: domain.LicenseBasic, Name: "Basic", Price: 1000, PriceCurrency: "INR", Prices: []money.StoredPrice{
			{Currency: "INR", AmountMinor: 100000, Revision: 1, IsActive: true}, {Currency: "USD", AmountMinor: 1499, Revision: 1, IsActive: true},
		}}}}
	require.NoError(t, application.NewSpecService(repo).CreateSpec(ctx, spec))
	fetched, err := repo.GetByID(ctx, spec.ID)
	require.NoError(t, err)
	require.Len(t, fetched.Licenses[0].Prices, 2)
	// A writer that thought the row was absent must not overwrite a concurrently
	// inserted first-revision row merely because both revisions happen to be 1.
	collision := fetched.Licenses[0].Prices[0]
	collision.ID = uuid.Nil
	collision.AmountMinor++
	require.ErrorIs(t, repo.UpsertLicensePrices(ctx, fetched.Licenses[0].ID, []money.StoredPrice{collision}), money.ErrStalePrice)

	byCurrency := func(prices []money.StoredPrice, currency string) money.StoredPrice {
		for _, p := range prices {
			if p.Currency == currency {
				return p
			}
		}
		t.Fatalf("missing %s", currency)
		return money.StoredPrice{}
	}
	oldUSD := byCurrency(fetched.Licenses[0].Prices, "USD")
	fetched.Licenses[0].Price = 1200
	fetched.Licenses[0].Prices = nil
	require.NoError(t, application.NewSpecService(repo).UpdateSpec(ctx, fetched, owner))
	reloaded, err := repo.GetByID(ctx, spec.ID)
	require.NoError(t, err)
	require.Equal(t, 120000, byCurrency(reloaded.Licenses[0].Prices, "INR").AmountMinor)
	require.Equal(t, oldUSD.ID, byCurrency(reloaded.Licenses[0].Prices, "USD").ID)
	require.Equal(t, oldUSD.Revision, byCurrency(reloaded.Licenses[0].Prices, "USD").Revision)
	listed, total, err := repo.List(ctx, domain.SpecFilter{Currency: "USD", MinPrice: 14, MaxPrice: 15, Limit: 20, Sort: "price_asc"})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, listed[0].Licenses[0].Prices, 2)
	_, total, err = repo.List(ctx, domain.SpecFilter{Currency: "INR", MinPrice: 14, MaxPrice: 15, Limit: 20})
	require.NoError(t, err)
	require.Zero(t, total)
	// Create a genuine missing canonical row, then audit/apply/retry/recover it.
	_, err = db.Exec(`DELETE FROM license_option_prices WHERE license_option_id=$1 AND currency='INR'`, reloaded.Licenses[0].ID)
	require.NoError(t, err)
	audit := priceaudit.NewService(pg.NewPriceAuditRepository(db))
	_, manifest, err := audit.Audit(ctx)
	require.NoError(t, err)
	require.Len(t, manifest.Changes, 1)
	n, err := audit.Apply(ctx, manifest)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	n, err = audit.Apply(ctx, manifest)
	require.NoError(t, err)
	require.Zero(t, n)
	n, err = audit.Rollback(ctx, manifest.BatchID)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	// A subsequent owner edit must block recovery instead of overwriting it.
	_, manifest, err = audit.Audit(ctx)
	require.NoError(t, err)
	_, err = audit.Apply(ctx, manifest)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE license_option_prices SET amount_minor=130000,amount_major=1300,revision=revision+1,updated_at=NOW() WHERE license_option_id=$1 AND currency='INR'`, reloaded.Licenses[0].ID)
	require.NoError(t, err)
	_, err = audit.Rollback(ctx, manifest.BatchID)
	require.Error(t, err)
}
