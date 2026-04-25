package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/featureflags"
)

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	return router
}

func TestFeatureFlag_Enabled(t *testing.T) {
	router := setupTestRouter()

	featureflags.SetFlag("test_enabled", true, "")

	router.GET("/test", FeatureFlag("test_enabled"), func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["message"] != "success" {
		t.Error("Expected success message")
	}
}

func TestFeatureFlag_Disabled(t *testing.T) {
	router := setupTestRouter()

	featureflags.SetFlag("test_disabled", false, "")

	router.GET("/test", FeatureFlag("test_disabled"), func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 503 {
		t.Errorf("Expected status 503, got %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["error"] != "feature_unavailable" {
		t.Error("Expected feature_unavailable error")
	}
}

func TestFeatureFlag_WithDefault(t *testing.T) {
	router := setupTestRouter()

	router.GET("/test", FeatureFlagWithDefault("nonexistent_flag", true), func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestFeatureFlag_WithDefaultFalse(t *testing.T) {
	router := setupTestRouter()

	router.GET("/test", FeatureFlagWithDefault("nonexistent_flag", false), func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 503 {
		t.Errorf("Expected status 503, got %d", w.Code)
	}
}

func TestFeatureFlag_EmptyFlagName(t *testing.T) {
	router := setupTestRouter()

	router.GET("/test", FeatureFlag(""), func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 500 {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}

func TestFeatureFlag_CustomResponse(t *testing.T) {
	router := setupTestRouter()

	featureflags.SetFlag("test_custom", false, "")

	customResponse := func(c *gin.Context) {
		c.JSON(418, gin.H{"custom": "response"})
	}

	router.GET("/test", FeatureFlagWithOptions(FeatureFlagOptions{
		FlagName:       "test_custom",
		DefaultEnabled: false,
		CustomResponse: customResponse,
		LogDisabled:    false,
	}), func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 418 {
		t.Errorf("Expected status 418, got %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["custom"] != "response" {
		t.Error("Expected custom response")
	}
}

func TestConditionalFeatureFlag_ConditionTrue(t *testing.T) {
	router := setupTestRouter()

	featureflags.SetFlag("test_conditional", false, "")

	condition := func(c *gin.Context) bool {
		return c.GetHeader("X-Test-Condition") == "true"
	}

	router.GET("/test", ConditionalFeatureFlag("test_conditional", condition), func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Test-Condition", "true")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestConditionalFeatureFlag_ConditionFalse(t *testing.T) {
	router := setupTestRouter()

	featureflags.SetFlag("test_conditional", false, "")

	condition := func(c *gin.Context) bool {
		return c.GetHeader("X-Test-Condition") == "true"
	}

	router.GET("/test", ConditionalFeatureFlag("test_conditional", condition), func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestRequireAnyFeatureFlag_OneEnabled(t *testing.T) {
	router := setupTestRouter()

	featureflags.SetFlag("flag1", false, "")
	featureflags.SetFlag("flag2", true, "")

	router.GET("/test", RequireAnyFeatureFlag("flag1", "flag2"), func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestRequireAnyFeatureFlag_NoneEnabled(t *testing.T) {
	router := setupTestRouter()

	featureflags.SetFlag("flag1", false, "")
	featureflags.SetFlag("flag2", false, "")

	router.GET("/test", RequireAnyFeatureFlag("flag1", "flag2"), func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 503 {
		t.Errorf("Expected status 503, got %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["error"] != "features_unavailable" {
		t.Error("Expected features_unavailable error")
	}
}

func TestRequireAllFeatureFlags_AllEnabled(t *testing.T) {
	router := setupTestRouter()

	featureflags.SetFlag("flag1", true, "")
	featureflags.SetFlag("flag2", true, "")

	router.GET("/test", RequireAllFeatureFlags("flag1", "flag2"), func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestRequireAllFeatureFlags_OneDisabled(t *testing.T) {
	router := setupTestRouter()

	featureflags.SetFlag("flag1", true, "")
	featureflags.SetFlag("flag2", false, "")

	router.GET("/test", RequireAllFeatureFlags("flag1", "flag2"), func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 503 {
		t.Errorf("Expected status 503, got %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["error"] != "feature_unavailable" {
		t.Error("Expected feature_unavailable error")
	}
	if response["missing_flag"] != "flag2" {
		t.Error("Expected missing_flag to be flag2")
	}
}

func TestRequireAnyFeatureFlags_EmptyList(t *testing.T) {
	router := setupTestRouter()

	router.GET("/test", RequireAnyFeatureFlag(), func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 500 {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}

func TestRequireAllFeatureFlags_EmptyList(t *testing.T) {
	router := setupTestRouter()

	router.GET("/test", RequireAllFeatureFlags(), func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 500 {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}
