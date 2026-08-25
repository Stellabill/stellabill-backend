package db

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/secrets"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCredentialDSN(t *testing.T) {
	dsn, err := buildCredentialDSN(
		"postgres://olduser:oldpass@db.internal:5432/app?sslmode=disable",
		secrets.DBCredential{Username: "v-app-abc", Password: "s3cret"},
	)
	require.NoError(t, err)
	assert.Contains(t, dsn, "v-app-abc")
	assert.Contains(t, dsn, "s3cret")
	assert.Contains(t, dsn, "db.internal:5432")
	assert.Contains(t, dsn, "sslmode=disable")
	assert.NotContains(t, dsn, "olduser")
	assert.NotContains(t, dsn, "oldpass")
}

func TestBuildCredentialDSN_InvalidBase(t *testing.T) {
	_, err := buildCredentialDSN("://not-a-dsn", secrets.DBCredential{Username: "u", Password: "p"})
	assert.Error(t, err)
}

func TestNewRotatingPool_EmptyDBConn(t *testing.T) {
	cfg := baseCfg()
	cfg.DBConn = ""
	pool, err := NewRotatingPool(context.Background(), cfg, make(chan secrets.DBCredential))
	assert.NoError(t, err)
	assert.Nil(t, pool)
}

func TestNewRotatingPool_NilChannel(t *testing.T) {
	cfg := baseCfg()
	pool, err := NewRotatingPool(context.Background(), cfg, nil)
	assert.Error(t, err)
	assert.Nil(t, pool)
}

func TestNewRotatingPool_ChannelClosedBeforeFirst(t *testing.T) {
	cfg := baseCfg()
	ch := make(chan secrets.DBCredential)
	close(ch)
	pool, err := NewRotatingPool(context.Background(), cfg, ch)
	assert.Error(t, err)
	assert.Nil(t, pool)
}

func TestNewRotatingPool_ContextCancelledBeforeFirst(t *testing.T) {
	cfg := baseCfg()
	ch := make(chan secrets.DBCredential)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pool, err := NewRotatingPool(ctx, cfg, ch)
	assert.Error(t, err)
	assert.Nil(t, pool)
}

func TestRotatingPool_CloseNilPool(t *testing.T) {
	rp := &RotatingPool{}
	err := rp.Close(context.Background())
	assert.NoError(t, err)
}

func TestRotatingPool_CloseTwice(t *testing.T) {
	rp := &RotatingPool{closed: true}
	err := rp.Close(context.Background())
	assert.NoError(t, err)
}

func TestRotatingPool_PoolNil(t *testing.T) {
	rp := &RotatingPool{}
	assert.Nil(t, rp.Pool())
}

// TestRotatingPool_Integration exercises the full rotation flow against a real
// database when DATABASE_URL is set. It verifies that the pool is usable after
// the first credential and that rotation swaps the underlying pool.
func TestRotatingPool_Integration(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}

	cfg := baseCfg()
	cfg.DBConn = dsn

	// Parse the base DSN to extract the real user/password so we can feed them
	// back as the "dynamic" credential.
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	baseUser := u.User.Username()
	basePass, _ := u.User.Password()

	ch := make(chan secrets.DBCredential, 2)
	ch <- secrets.DBCredential{
		Username:  baseUser,
		Password:  basePass,
		LeaseID:   "lease1",
		ExpiresAt: time.Now().Add(time.Hour),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rp, err := NewRotatingPool(ctx, cfg, ch)
	require.NoError(t, err)
	require.NotNil(t, rp)
	defer func() {
		_ = rp.Close(context.Background())
	}()

	pool := rp.Pool()
	require.NotNil(t, pool)
	require.NoError(t, pool.Ping(context.Background()))

	// Push a second credential to trigger rotation.
	ch <- secrets.DBCredential{
		Username:  baseUser,
		Password:  basePass,
		LeaseID:   "lease2",
		ExpiresAt: time.Now().Add(time.Hour),
	}

	// Wait for rotation to complete.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		newPool := rp.Pool()
		if newPool != nil && newPool != pool {
			require.NoError(t, newPool.Ping(context.Background()))
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("pool was not rotated after new credential")
}
