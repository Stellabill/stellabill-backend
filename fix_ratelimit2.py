import re

with open("internal/routes/ratelimit_integration_test.go", "r") as f:
    content = f.read()

# Replace user123 with user456 for req2 in User mode
target = """		req2 := newAuthRequest("GET", path)
		req2.RemoteAddr = "1.1.1.1:1234" // Same IP
"""
replacement = """		req2 := newAuthRequest("GET", path)
		token2, _ := createToken("Test1!JwtSecret-MixedAlphaNumeric@123", "user456", []auth.Role{auth.RoleUser}, time.Now().Add(time.Hour))
		req2.Header.Set("Authorization", "Bearer " + token2)
		req2.RemoteAddr = "1.1.1.1:1234" // Same IP
"""
content = content.replace(target, replacement)

# Do the same for hybrid mode?
target2 = """		req2 := newAuthRequest("GET", path)
		req2.RemoteAddr = "2.2.2.2:1234" // Different IP
"""
replacement2 = """		req2 := newAuthRequest("GET", path)
		token2, _ := createToken("Test1!JwtSecret-MixedAlphaNumeric@123", "user456", []auth.Role{auth.RoleUser}, time.Now().Add(time.Hour))
		req2.Header.Set("Authorization", "Bearer " + token2)
		req2.RemoteAddr = "2.2.2.2:1234" // Different IP
"""
content = content.replace(target2, replacement2)

with open("internal/routes/ratelimit_integration_test.go", "w") as f:
    f.write(content)

with open("internal/routes/auth_integration_test.go", "r") as f:
    auth_content = f.read()

# disable rate limiting in auth test
auth_setup = """	os.Setenv("RATE_LIMIT_ENABLED", "false")
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")"""

auth_content = auth_content.replace('os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")', auth_setup)

with open("internal/routes/auth_integration_test.go", "w") as f:
    f.write(auth_content)
