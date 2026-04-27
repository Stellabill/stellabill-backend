package middleware

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"stellabill-backend/internal/featureflags"
)

type FeatureFlagOptions struct {
	FlagName       string
	DefaultEnabled bool
	CustomResponse func(*gin.Context)
	LogDisabled    bool
}

func FeatureFlag(flagName string) gin.HandlerFunc {
	return FeatureFlagWithOptions(FeatureFlagOptions{
		FlagName:       flagName,
		DefaultEnabled: false,
		LogDisabled:    true,
	})
}

func FeatureFlagWithDefault(flagName string, defaultEnabled bool) gin.HandlerFunc {
	return FeatureFlagWithOptions(FeatureFlagOptions{
		FlagName:       flagName,
		DefaultEnabled: defaultEnabled,
		LogDisabled:    true,
	})
}

func FeatureFlagWithOptions(options FeatureFlagOptions) gin.HandlerFunc {
	return func(c *gin.Context) {
		if options.FlagName == "" {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "feature flag middleware: flag name cannot be empty",
			})
			c.Abort()
			return
		}

		enabled := featureflags.IsEnabledWithDefault(options.FlagName, options.DefaultEnabled)

		if !enabled {
			if options.LogDisabled {
				log.Printf("Feature flag '%s' is disabled, blocking request to %s", options.FlagName, c.Request.URL.Path)
			}

			if options.CustomResponse != nil {
				options.CustomResponse(c)
			} else {
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"error":        "feature_unavailable",
					"message":      "This feature is currently unavailable",
					"feature_flag": options.FlagName,
				})
			}
			c.Abort()
			return
		}

		c.Next()
	}
}

func ConditionalFeatureFlag(flagName string, condition func(*gin.Context) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if condition != nil && !condition(c) {
			c.Next()
			return
		}

		enabled := featureflags.IsEnabled(flagName)
		if !enabled {
			log.Printf("Feature flag '%s' is disabled, blocking request to %s", flagName, c.Request.URL.Path)
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":        "feature_unavailable",
				"message":      "This feature is currently unavailable",
				"feature_flag": flagName,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

func RequireAnyFeatureFlag(flagNames ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if len(flagNames) == 0 {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "feature flag middleware: at least one flag name must be provided",
			})
			c.Abort()
			return
		}

		for _, flagName := range flagNames {
			if featureflags.IsEnabled(flagName) {
				c.Next()
				return
			}
		}

		log.Printf("All required feature flags %v are disabled, blocking request to %s", flagNames, c.Request.URL.Path)
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":          "features_unavailable",
			"message":        "None of the required features are currently available",
			"required_flags": flagNames,
		})
		c.Abort()
	}
}

func RequireAllFeatureFlags(flagNames ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if len(flagNames) == 0 {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "feature flag middleware: at least one flag name must be provided",
			})
			c.Abort()
			return
		}

		for _, flagName := range flagNames {
			if !featureflags.IsEnabled(flagName) {
				log.Printf("Feature flag '%s' is disabled, blocking request to %s", flagName, c.Request.URL.Path)
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"error":          "feature_unavailable",
					"message":        "Required feature is currently unavailable",
					"missing_flag":   flagName,
					"required_flags": flagNames,
				})
				c.Abort()
				return
			}
		}

		c.Next()
	}
}

