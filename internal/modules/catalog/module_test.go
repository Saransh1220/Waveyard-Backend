package catalog

import (
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"
	persistence "github.com/saransh1220/blueprint-audio/internal/modules/catalog/infrastructure/persistence/postgres"
	"github.com/stretchr/testify/require"
)

func TestModuleAccessors(t *testing.T) {
	repo := persistence.NewSpecRepository(&sqlx.DB{})
	m := NewModule(&sqlx.DB{}, repo, nil, nil, nil, &redis.Client{})
	require.NotNil(t, m)
	require.NotNil(t, m.Repository())
	require.NotNil(t, m.SpecFinder())
	require.NotNil(t, m.Service())
	require.NotNil(t, m.HTTPHandler())
	require.NotNil(t, m.UploadRepository())
	require.NotNil(t, m.UploadService())
	require.NotNil(t, m.UploadHTTPHandler())
}
