//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const startupTimeout = 90 * time.Second

// ContainerSpec is the generic factory input shared by every integration
// dependency. Host ports are intentionally omitted so Docker always selects an
// available port, including when multiple test processes run in parallel.
type ContainerSpec struct {
	Name         string
	Image        string
	Port         string
	Environment  map[string]string
	Command      []string
	WaitStrategy wait.Strategy
}

// StartContainer creates a reusable container from a small, reviewable spec.
func StartContainer(ctx context.Context, spec ContainerSpec) (testcontainers.Container, string, error) {
	if spec.Name == "" || spec.Image == "" || spec.Port == "" || spec.WaitStrategy == nil {
		return nil, "", fmt.Errorf("container name, image, port, and wait strategy are required")
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Name:         spec.Name,
			Image:        spec.Image,
			ExposedPorts: []string{spec.Port},
			Env:          spec.Environment,
			Cmd:          spec.Command,
			WaitingFor:   spec.WaitStrategy,
			HostConfigModifier: func(hostConfig *container.HostConfig) {
				// Do not expose integration services to the LAN. An empty
				// HostPort still asks Docker to allocate a collision-free port.
				hostConfig.PortBindings = nat.PortMap{
					nat.Port(spec.Port): {{HostIP: "127.0.0.1"}},
				}
			},
		},
		Started: true,
		Reuse:   true,
	})
	if err != nil {
		return nil, "", fmt.Errorf("start %s: %w", spec.Image, err)
	}

	endpoint, err := container.Endpoint(ctx, "")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", fmt.Errorf("resolve %s endpoint: %w", spec.Image, err)
	}
	return container, endpoint, nil
}

type IntegrationStack struct {
	PostgresURL string
	RedisURL    string
	WebhookURL  string

	containers []testcontainers.Container
}

func StartIntegrationStack(ctx context.Context) (*IntegrationStack, error) {
	dbPassword, err := randomSecret()
	if err != nil {
		return nil, err
	}
	redisPassword, err := randomSecret()
	if err != nil {
		return nil, err
	}
	runID, err := randomSecret()
	if err != nil {
		return nil, err
	}
	runID = runID[:12]

	stack := &IntegrationStack{}
	start := func(spec ContainerSpec) (string, error) {
		container, endpoint, startErr := StartContainer(ctx, spec)
		if startErr == nil {
			stack.containers = append(stack.containers, container)
		}
		return endpoint, startErr
	}

	pgEndpoint, err := start(ContainerSpec{
		Name:  "stellabill-integration-postgres-" + runID,
		Image: "postgres:16.9-alpine",
		Port:  "5432/tcp",
		Environment: map[string]string{
			"POSTGRES_DB":       "stellabill_test",
			"POSTGRES_USER":     "stellabill",
			"POSTGRES_PASSWORD": dbPassword,
		},
		WaitStrategy: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(startupTimeout),
	})
	if err != nil {
		stack.Close(ctx)
		return nil, err
	}
	pgURL := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword("stellabill", dbPassword),
		Host:     pgEndpoint,
		Path:     "stellabill_test",
		RawQuery: "sslmode=disable",
	}
	stack.PostgresURL = pgURL.String()

	redisEndpoint, err := start(ContainerSpec{
		Name:         "stellabill-integration-redis-" + runID,
		Image:        "redis:7.4.5-alpine",
		Port:         "6379/tcp",
		Command:      []string{"redis-server", "--save", "", "--appendonly", "no", "--requirepass", redisPassword},
		WaitStrategy: wait.ForListeningPort("6379/tcp").WithStartupTimeout(startupTimeout),
	})
	if err != nil {
		stack.Close(ctx)
		return nil, err
	}
	redisURL := &url.URL{Scheme: "redis", User: url.UserPassword("", redisPassword), Host: redisEndpoint}
	stack.RedisURL = redisURL.String()

	webhookEndpoint, err := start(ContainerSpec{
		Name:         "stellabill-integration-webhook-" + runID,
		Image:        "mendhak/http-https-echo:35",
		Port:         "8080/tcp",
		WaitStrategy: wait.ForHTTP("/").WithPort("8080/tcp").WithStartupTimeout(startupTimeout),
	})
	if err != nil {
		stack.Close(ctx)
		return nil, err
	}
	stack.WebhookURL = "http://" + webhookEndpoint
	return stack, nil
}

func (s *IntegrationStack) Close(ctx context.Context) {
	for i := len(s.containers) - 1; i >= 0; i-- {
		_ = s.containers[i].Terminate(ctx)
	}
}

func (s *IntegrationStack) PingPostgres(ctx context.Context) error {
	db, err := sql.Open("postgres", s.PostgresURL)
	if err != nil {
		return err
	}
	defer db.Close()
	return db.PingContext(ctx)
}

func (s *IntegrationStack) PingRedis(ctx context.Context) error {
	options, err := redis.ParseURL(s.RedisURL)
	if err != nil {
		return err
	}
	client := redis.NewClient(options)
	defer client.Close()
	return client.Ping(ctx).Err()
}

func (s *IntegrationStack) PingWebhook(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.WebhookURL+"/integration", strings.NewReader(`{"event":"test"}`))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("mock webhook returned %s", response.Status)
	}
	return nil
}

func randomSecret() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate test credential: %w", err)
	}
	return hex.EncodeToString(value), nil
}
