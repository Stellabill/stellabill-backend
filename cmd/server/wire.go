//go:build wireinject
// +build wireinject

// The build constraints above ensure this file is compiled ONLY by the wire
// tool, not by the normal Go compiler.  The generated wire_gen.go file takes
// its place for all other build targets.
//
// To regenerate wire_gen.go run:
//
//	go generate ./cmd/server/...
//
// or, from the repo root:
//
//	wire gen ./cmd/server/
package main

import (
	"net/http"

	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AppProviders is the complete set of constructor functions that wire uses to
// resolve the dependency graph.  Adding a new top-level dependency is a
// single-line addition here.
//
//go:generate wire gen .
var AppProviders = wire.NewSet(
	ProvideConfig,
	ProvideRouter,
	ProvideDBPool,
	ProvideReplicaPool,
	ProvideHTTPServer,
)

// InitializeServer is the wire injector.  The body below is a stub; wire
// replaces it with the generated implementation in wire_gen.go.
//
// The function signature is the public contract:
//   - no inputs – all values come from the provider chain.
//   - returns (primary, replica *pgxpool.Pool, *http.Server, error). Both pools
//     are returned so main() can drain them during graceful shutdown. The
//     replica pool is nil when no replica is configured.
func InitializeServer() (*pgxpool.Pool, *pgxpool.Pool, *http.Server, error) {
	wire.Build(AppProviders)
	return nil, nil, nil, nil
}
