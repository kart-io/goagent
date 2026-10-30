package postgres

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
)

func TestDSNEndpoint(t *testing.T) {
	assert.Equal(t, "db.internal/app", dsnEndpoint("host=db.internal user=admin password=supersecret dbname=app sslmode=disable"))
	assert.Equal(t, "db.internal/app", dsnEndpoint("postgres://admin:supersecret@db.internal:5432/app?sslmode=disable"))
	assert.Equal(t, "", dsnEndpoint("host=db.internal password=supersecret port=notaport"))
}

// 连接失败时 key=value 形式 DSN 中的密码不得进入 Context 与 Error()。
func TestNewConnectionErrorOmitsPassword(t *testing.T) {
	for name, dsn := range map[string]string{
		"解析失败": "host=127.0.0.1 user=admin password=supersecret dbname=app port=notaport",
		"连接失败": "host=127.0.0.1 port=1 user=admin password=supersecret dbname=app sslmode=disable connect_timeout=1",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newFromConfig(&Config{DSN: dsn})
			require.Error(t, err)
			require.True(t, agentErrors.IsCode(err, agentErrors.CodeStoreConnection))
			assert.NotContains(t, err.Error(), "supersecret")
			for k, v := range agentErrors.GetContext(err) {
				assert.NotContains(t, v, "supersecret", "context %s", k)
			}
		})
	}

	_, err := newFromConfig(&Config{DSN: "host=127.0.0.1 port=1 user=admin password=supersecret dbname=app sslmode=disable connect_timeout=1"})
	assert.Equal(t, "127.0.0.1/app", agentErrors.GetContext(err)["endpoint"])

	_, err = newFromConfig(&Config{DSN: "host=127.0.0.1 password=supersecret port=notaport"})
	assert.NotContains(t, agentErrors.GetContext(err), "endpoint", "解析失败时不放 endpoint")
}
