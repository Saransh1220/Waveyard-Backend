package promotion

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/saransh1220/blueprint-audio/internal/gateway/middleware"
	catalogdomain "github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
	catalogpostgres "github.com/saransh1220/blueprint-audio/internal/modules/catalog/infrastructure/persistence/postgres"
	"github.com/saransh1220/blueprint-audio/internal/modules/promotion/application"
	"github.com/saransh1220/blueprint-audio/internal/modules/promotion/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/promotion/infrastructure/persistence/postgres"
	"github.com/stretchr/testify/require"
)

func TestPromotionProducerIsolationAndRevisions(t *testing.T) {
	dsn := os.Getenv("TEST_PROMOTIONS_DB_URL")
	if dsn == "" {
		t.Skip("TEST_PROMOTIONS_DB_URL must point to a disposable, migrated PostgreSQL database")
	}
	db, err := sqlx.Connect("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	producer, beat, option := seedCatalog(t, db)
	other, foreignBeat, foreignOption := seedCatalog(t, db)
	list, total, err := catalogpostgres.NewSpecRepository(db).ListByUserID(context.Background(), producer, 1, 0,
		catalogdomain.ProducerSpecFilter{BeatsOnly: true, Search: "raC", IDs: []uuid.UUID{beat, foreignBeat}})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, list, 1)
	require.Equal(t, beat, list[0].ID)
	h := NewHandler(db)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /studio/promotions", h.Create)
	mux.HandleFunc("GET /studio/promotions", h.List)
	mux.HandleFunc("GET /studio/promotions/{id}", h.Get)
	mux.HandleFunc("GET /studio/promotions/{id}/preview", h.Preview)
	mux.HandleFunc("POST /studio/promotions/{id}/enrollment-check", h.CheckEnrollment)
	mux.HandleFunc("PATCH /studio/promotions/{id}", h.Change)
	request := func(actor uuid.UUID, method, path string, input any) *httptest.ResponseRecorder {
		body, _ := json.Marshal(input)
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		ctx, cancel := context.WithTimeout(req.Context(), 10*time.Second)
		defer cancel()
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req.WithContext(context.WithValue(ctx, middleware.ContextKeyUserId, actor)))
		return res
	}
	rules := domain.Rules{
		Name: "Studio sale", PercentBPS: 2500, Currencies: []string{"USD"},
		Enrollments: []domain.Enrollment{{BeatID: beat, OptionIDs: []uuid.UUID{option}}},
	}
	const collection = "/studio/promotions"
	require.Equal(t, 401, request(uuid.Nil, "POST", collection, rules).Code)
	forged := request(producer, "POST", collection, map[string]any{"producer_id": other})
	require.Equal(t, 400, forged.Code)
	foreign := rules
	foreign.Enrollments = []domain.Enrollment{{BeatID: foreignBeat, AllPaid: true}}
	require.Equal(t, 400, request(producer, "POST", collection, foreign).Code)
	foreign.Enrollments = []domain.Enrollment{{BeatID: beat, OptionIDs: []uuid.UUID{foreignOption}}}
	require.Equal(t, 400, request(producer, "POST", collection, foreign).Code)

	res := request(producer, "POST", collection, rules)
	require.Equal(t, 201, res.Code, res.Body.String())
	var promotion domain.Promotion
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &promotion))
	require.Equal(t, producer, promotion.ProducerID)
	require.Equal(t, int64(1), promotion.Revision)
	require.Equal(t, domain.Draft, promotion.State)
	path := collection + "/" + promotion.ID.String()
	require.Equal(t, 404, request(other, "GET", path, nil).Code)
	require.Equal(t, 404, request(other, "GET", path+"/preview", nil).Code)
	require.Equal(t, 404, request(other, "POST", path+"/enrollment-check", domain.EnrollmentCheckRequest{BeatIDs: []uuid.UUID{beat}}).Code)
	require.Equal(t, 404, request(other, "PATCH", path, domain.ChangeRequest{Revision: 1, Action: domain.Publish}).Code)
	require.JSONEq(t, `{"data":[]}`, request(other, "GET", collection, nil).Body.String())

	res = request(producer, "GET", path+"/preview", nil)
	require.Equal(t, 200, res.Code, res.Body.String())
	var preview domain.Preview
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &preview))
	require.Equal(t, rules.Enrollments, preview.Eligible)

	invalid := rules
	invalid.Enrollments = append(append([]domain.Enrollment{}, rules.Enrollments...), domain.Enrollment{BeatID: foreignBeat, AllPaid: true})
	res = request(producer, "PATCH", path, domain.ChangeRequest{Revision: 1, Action: domain.Edit, Rules: &invalid})
	require.Equal(t, 400, res.Code)

	type result struct {
		status int
		body   string
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			response := request(producer, "PATCH", path, domain.ChangeRequest{Revision: 1, Action: domain.Publish})
			results <- result{response.Code, response.Body.String()}
		}()
	}
	first, second := <-results, <-results
	require.ElementsMatch(t, []int{200, 409}, []int{first.status, second.status}, "%+v %+v", first, second)

	rules.PercentBPS = 3000
	res = request(producer, "PATCH", path, domain.ChangeRequest{Revision: 2, Action: domain.Edit, Rules: &rules})
	require.Equal(t, 200, res.Code, res.Body.String())
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &promotion))
	require.Equal(t, int64(3), promotion.Revision)
	require.NotNil(t, promotion.PublishedRevision)
	require.Equal(t, int64(2), *promotion.PublishedRevision)
	require.Equal(t, domain.Draft, promotion.State)
	_, err = db.Exec("UPDATE specs SET producer_id=$1 WHERE id=$2", other, beat)
	require.NoError(t, err)
	require.Equal(t, 400, request(producer, "PATCH", path, domain.ChangeRequest{Revision: 3, Action: domain.Publish}).Code)
	require.Equal(t, 200, request(producer, "PATCH", path, domain.ChangeRequest{Revision: 3, Action: domain.Pause}).Code)
	require.Equal(t, 200, request(producer, "PATCH", path, domain.ChangeRequest{Revision: 4, Action: domain.Archive}).Code)
	require.Equal(t, 409, request(producer, "PATCH", path, domain.ChangeRequest{Revision: 5, Action: domain.Publish}).Code)

	var actions []string
	err = db.Select(&actions, "SELECT action FROM producer_promotion_revisions WHERE promotion_id=$1 ORDER BY revision", promotion.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"create", "publish", "edit", "pause", "archive"}, actions)
}

func TestPromotionPublicationSerializesOverlappingBeatWindows(t *testing.T) {
	dsn := os.Getenv("TEST_PROMOTIONS_DB_URL")
	if dsn == "" {
		t.Skip("TEST_PROMOTIONS_DB_URL must point to a disposable, migrated PostgreSQL database")
	}
	db, err := sqlx.Connect("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	producer, beat, _ := seedCatalog(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	now := time.Now().UTC()
	end := now.Add(2 * time.Hour)
	rules := domain.Rules{
		Name: "Original", PercentBPS: 2500, Currencies: []string{"USD"}, EndsAt: &end,
		Enrollments: []domain.Enrollment{{BeatID: beat, AllPaid: true}},
	}
	service := application.NewService(postgres.NewRepository(db, catalogpostgres.LockPromotionOptions), func() time.Time { return now })
	a, err := service.Create(ctx, producer, rules)
	require.NoError(t, err)
	rules.Name = "Second"
	b, err := service.Create(ctx, producer, rules)
	require.NoError(t, err)

	type attempt struct {
		coupon domain.Promotion
		err    error
	}
	results := make(chan attempt, 2)
	for _, coupon := range []domain.Promotion{a, b} {
		go func() {
			_, err := service.Change(ctx, producer, coupon.ID, coupon.Revision, domain.Publish, nil)
			results <- attempt{coupon, err}
		}()
	}
	success, failed := <-results, <-results
	if success.err != nil {
		success, failed = failed, success
	}
	require.NoError(t, success.err)
	var collision *domain.EnrollmentConflictError
	require.ErrorAs(t, failed.err, &collision)
	require.Equal(t, []domain.EnrollmentConflict{{BeatID: beat, CouponID: success.coupon.ID, CouponName: success.coupon.Rules.Name}}, collision.Conflicts)

	missing := uuid.New()
	check, err := service.CheckEnrollment(ctx, producer, failed.coupon.ID, []uuid.UUID{beat, missing})
	require.NoError(t, err)
	require.Equal(t, collision.Conflicts, check.Conflicts)
	require.Equal(t, []uuid.UUID{missing}, check.UnavailableBeatIDs)
	_, err = service.Change(ctx, producer, failed.coupon.ID, 1, domain.Edit, &rules)
	require.ErrorAs(t, err, &collision)

	// A schedule starting exactly when the first ends can use the same beat.
	laterEnd := end.Add(time.Hour)
	rules.StartsAt, rules.EndsAt = &end, &laterEnd
	later, err := service.Change(ctx, producer, failed.coupon.ID, 1, domain.Edit, &rules)
	require.NoError(t, err)
	_, err = service.Change(ctx, producer, later.ID, later.Revision, domain.Publish, nil)
	require.NoError(t, err)
}

func seedCatalog(t *testing.T, db *sqlx.DB) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	producer, beat, option := uuid.New(), uuid.New(), uuid.New()
	_, err := db.Exec(`INSERT INTO users(id,email,password_hash,name,role,display_name,handle)
		VALUES($1,$2,'hash','Test','producer','Test',$3)`, producer, producer.String()+"@example.test", "producer"+producer.String()[:8])
	require.NoError(t, err)
	t.Cleanup(func() {
		tx, err := db.Beginx()
		require.NoError(t, err)
		defer tx.Rollback()
		_, err = tx.Exec(`DELETE FROM producer_promotion_revisions WHERE actor_id=$1`, producer)
		require.NoError(t, err)
		_, err = tx.Exec(`DELETE FROM producer_promotions WHERE producer_id=$1`, producer)
		require.NoError(t, err)
		_, err = tx.Exec(`DELETE FROM users WHERE id=$1`, producer)
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
	})
	_, err = db.Exec(`INSERT INTO specs(id,producer_id,title,category,type,bpm,key,base_price,image_url,preview_url,processing_status)
		VALUES($1,$2,'Track','beat','WAV',120,'C',10,'image','preview','completed')`, beat, producer)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO license_options(id,spec_id,license_type,name,price,features,file_types)
		VALUES($1,$2,'Premium','WAV',10,'{}','{WAV}')`, option, beat)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO license_option_prices(id,license_option_id,currency,amount_minor,amount_major)
		VALUES($1,$2,'USD',1000,10)`, uuid.New(), option)
	require.NoError(t, err)
	return producer, beat, option
}
