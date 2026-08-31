package middleware

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/featureflags"

	"github.com/gin-gonic/gin"
)

const (
	faultHeader     = "X-Stellabill-Inject"
	maxFaultLatency = 5 * time.Second
)

// FaultSpec captures a single allowed fault injection directive.
type FaultSpec struct {
	Latency time.Duration
	Status  int
	Prob    float64
	Cancel  bool
}

func FaultInjection() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !featureflags.IsFaultInjectionEnabled() {
			c.Next()
			return
		}

		if !isAdminCaller(c) {
			c.Next()
			return
		}

		header := strings.TrimSpace(c.GetHeader(faultHeader))
		if header == "" {
			c.Next()
			return
		}

		cfg, err := ParseFaultHeader(header)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_fault_injection_header",
				"detail":  err.Error(),
				"header":  header,
				"allowed": "latency=<duration>,status=<500-599>,prob=<0.0-1.0>,cancel=true|false",
			})
			return
		}

		if cfg.Prob == 0 {
			c.Next()
			return
		}

		if cfg.Prob > 0 && rand.Float64() > cfg.Prob {
			c.Next()
			return
		}

		if cfg.Latency > 0 {
			if cfg.Latency > maxFaultLatency {
				cfg.Latency = maxFaultLatency
			}
			time.Sleep(cfg.Latency)
		}

		if cfg.Cancel {
			ctx, cancel := context.WithCancel(c.Request.Context())
			c.Request = c.Request.WithContext(ctx)
			cancel()
		}

		if cfg.Status >= 500 && cfg.Status <= 599 {
			c.AbortWithStatus(cfg.Status)
			return
		}

		c.Next()
	}
}

// ParseFaultHeader validates and normalizes an X-Stellabill-Inject header.
func ParseFaultHeader(header string) (FaultSpec, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return FaultSpec{}, nil
	}

	cfg := FaultSpec{}
	seen := false
	for _, pair := range strings.Split(header, ",") {
		trimmed := strings.TrimSpace(pair)
		if trimmed == "" {
			continue
		}

		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) != 2 {
			return FaultSpec{}, fmt.Errorf("invalid fault injection segment %q: expected key=value", trimmed)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" || value == "" {
			return FaultSpec{}, fmt.Errorf("invalid fault injection segment %q: empty key or value", trimmed)
		}

		switch key {
		case "latency":
			d, err := time.ParseDuration(value)
			if err != nil || d < 0 {
				return FaultSpec{}, fmt.Errorf("invalid latency %q: must be a non-negative duration", value)
			}
			cfg.Latency = d
			seen = true
		case "status":
			s, err := strconv.Atoi(value)
			if err != nil || s < 100 || s > 599 {
				return FaultSpec{}, fmt.Errorf("invalid status %q: must be an integer between 100 and 599", value)
			}
			cfg.Status = s
			seen = true
		case "prob":
			p, err := strconv.ParseFloat(value, 64)
			if err != nil || p < 0 || p > 1 {
				return FaultSpec{}, fmt.Errorf("invalid probability %q: must be a float between 0 and 1", value)
			}
			cfg.Prob = p
			seen = true
		case "cancel":
			ok, err := strconv.ParseBool(value)
			if err != nil {
				return FaultSpec{}, fmt.Errorf("invalid cancel %q: must be true or false", value)
			}
			cfg.Cancel = ok
			seen = true
		default:
			return FaultSpec{}, fmt.Errorf("unsupported fault injection key %q", key)
		}
	}

	if !seen {
		return FaultSpec{}, errors.New("no recognized fault injection directives present")
	}

	if cfg.Latency > maxFaultLatency {
		cfg.Latency = maxFaultLatency
	}
	return cfg, nil
}

func isAdminCaller(c *gin.Context) bool {
	for _, role := range auth.ExtractRoles(c) {
		if strings.EqualFold(string(role), "admin") {
			return true
		}
	}
	return false
}

func parseFaultHeader(header string) FaultSpec {
	cfg, _ := ParseFaultHeader(header)
	return cfg
}
