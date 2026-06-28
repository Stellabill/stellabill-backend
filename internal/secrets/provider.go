package secrets

import (
	"context"
	"errors"
)

var ErrSecretNotFound = errors.New("secret not found")
var ErrProviderTimeout = errors.New("secret provider timeout")

type Provider interface {
	GetSecret(ctx context.Context, key string) (string, error)
	Name() string
}