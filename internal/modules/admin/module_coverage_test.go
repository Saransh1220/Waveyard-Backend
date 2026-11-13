package admin

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModuleConstructionAndHandlerAccess(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer sqlDB.Close()
	db := sqlx.NewDb(sqlDB, "sqlmock")

	module := NewModule(db, nil, nil)
	assert.NotNil(t, module)
	assert.NotNil(t, module.HTTPHandler())
	assert.NotNil(t, module.AuditWriter())

	actorID := uuid.New()
	resID := uuid.New()
	mock.ExpectExec(`INSERT INTO admin_audit_logs`).
		WithArgs(&actorID, "update", "order", &resID, sqlmock.AnyArg(), sqlmock.AnyArg(), "127.0.0.1", "agent").
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = module.AuditWriter().Log(context.Background(), &actorID, "update", "order", resID.String(), map[string]string{"a": "b"}, map[string]string{"a": "c"}, "127.0.0.1", "agent")
	require.NoError(t, err)
}
