package routes

import (
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCoverage_Register(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost/db")
	os.Setenv("JWT_SECRET", "Test1!JwtSecret-MixedAlphaNumeric@123")
	os.Setenv("ADMIN_TOKEN", "Admin1!Token-MixedAlphaNumeric@123")
	defer os.Unsetenv("DATABASE_URL")
	defer os.Unsetenv("JWT_SECRET")
	defer os.Unsetenv("ADMIN_TOKEN")

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Register(r)
}
