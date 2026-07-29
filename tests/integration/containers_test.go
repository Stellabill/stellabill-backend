//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/wait"
)

var integrationStack *IntegrationStack

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	stack, err := StartIntegrationStack(ctx)
	if err != nil {
		panic("start integration stack: " + err.Error())
	}
	integrationStack = stack

	_ = os.Setenv("DATABASE_URL", stack.PostgresURL)
	_ = os.Setenv("REDIS_URL", stack.RedisURL)
	_ = os.Setenv("OUTBOX_HTTP_ENDPOINT", stack.WebhookURL)

	exitCode := m.Run()
	stack.Close(context.Background())
	os.Exit(exitCode)
}

func TestContainerFactoryRejectsIncompleteSpecs(t *testing.T) {
	t.Parallel()
	valid := ContainerSpec{
		Name:         "valid",
		Image:        "example.invalid/image:1",
		Port:         "8080/tcp",
		WaitStrategy: wait.ForListeningPort("8080/tcp"),
	}
	tests := []struct {
		name   string
		mutate func(*ContainerSpec)
	}{
		{name: "missing name", mutate: func(spec *ContainerSpec) { spec.Name = "" }},
		{name: "missing image", mutate: func(spec *ContainerSpec) { spec.Image = "" }},
		{name: "missing port", mutate: func(spec *ContainerSpec) { spec.Port = "" }},
		{name: "missing wait strategy", mutate: func(spec *ContainerSpec) { spec.WaitStrategy = nil }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := valid
			test.mutate(&spec)
			_, _, err := StartContainer(context.Background(), spec)
			require.Error(t, err)
		})
	}
}

func TestContainerFactoryAllocatesDistinctHostPorts(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	firstID, err := randomSecret()
	require.NoError(t, err)
	secondID, err := randomSecret()
	require.NoError(t, err)

	first, firstEndpoint, err := StartContainer(ctx, ContainerSpec{
		Name:  "stellabill-port-test-" + firstID[:12],
		Image: "mendhak/http-https-echo:35", Port: "8080/tcp",
		WaitStrategy: wait.ForHTTP("/").WithPort("8080/tcp").WithStartupTimeout(startupTimeout),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Terminate(context.Background()) })

	second, secondEndpoint, err := StartContainer(ctx, ContainerSpec{
		Name:  "stellabill-port-test-" + secondID[:12],
		Image: "mendhak/http-https-echo:35", Port: "8080/tcp",
		WaitStrategy: wait.ForHTTP("/").WithPort("8080/tcp").WithStartupTimeout(startupTimeout),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Terminate(context.Background()) })

	require.NotEqual(t, firstEndpoint, secondEndpoint, "Docker-assigned host ports must not collide")
}

func requireStack(t *testing.T) *IntegrationStack {
	t.Helper()
	require.NotNil(t, integrationStack)
	return integrationStack
}
