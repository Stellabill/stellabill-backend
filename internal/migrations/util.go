package migrations

import (
	"net/url"
	"strings"
)

// RedactDatabaseURL removes password/userinfo from DSNs for safe logging.
func RedactDatabaseURL(databaseURL string) string {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return "<invalid database url>"
	}
	if u.User != nil {
		user := u.User.Username()
		if user != "" {
			u.User = url.User(user)
		} else {
			u.User = nil
		}
	}
	s := u.String()
	if strings.Contains(s, "@") && strings.Contains(s, "://") {
		return s
	}
	return s
}

