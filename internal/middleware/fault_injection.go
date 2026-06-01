package middleware

import (
	"context"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/featureflags"
)

const (
	faultHeader = "X-Stellabill-Inject"
)

type faultConfig struct {
	latency   time.Duration
	status    int
	prob      float64
	cancelCtx bool
}

func FaultInjection() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !featureflags.IsEnabled("fault_injection_enabled") {
			c.Next()
			return
		}

		header := c.GetHeader(faultHeader)
		if header == "" {
			c.Next()
			return
		}

		cfg := parseFaultHeader(header)

		if cfg.prob > 0 && rand.Float64() > cfg.prob {
			c.Next()
			return
		}

		if cfg.latency > 0 {
			time.Sleep(cfg.latency)
		}

		if cfg.cancelCtx {
			ctx, cancel := context.WithCancel(c.Request.Context())
			c.Request = c.Request.WithContext(ctx)
			cancel()
		}

		if cfg.status >= 500 && cfg.status <= 599 {
			c.AbortWithStatus(cfg.status)
			return
		}

		c.Next()
	}
}

func parseFaultHeader(header string) faultConfig {
	var cfg faultConfig
	pairs := strings.Split(header, ",")
	for _, pair := range pairs {
		kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])

		switch key {
		case "latency":
			if d, err := time.ParseDuration(value); err == nil {
				cfg.latency = d
			}
		case "status":
			if s, err := strconv.Atoi(value); err == nil {
				cfg.status = s
			}
		case "prob":
			if p, err := strconv.ParseFloat(value, 64); err == nil {
				cfg.prob = p
			}
		case "cancel":
			if cancel, err := strconv.ParseBool(value); err == nil {
				cfg.cancelCtx = cancel
			}
		}
	}
	return cfg
}
