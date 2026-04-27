package requestparams

import (
	"net/url"
	"strings"
	"testing"
)

func TestNormalizePathID(t *testing.T) {
	t.Run("trims and normalizes unicode", func(t *testing.T) {
		got, err := NormalizePathID("id", " ｓｕｂ＿１２３ ")
		if err != nil {
			t.Fatalf("NormalizePathID returned error: %v", err)
		}
		if got != "sub_123" {
			t.Fatalf("NormalizePathID = %q, want %q", got, "sub_123")
		}
	})

	t.Run("rejects invalid characters", func(t *testing.T) {
		_, err := NormalizePathID("id", "<script>")
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "letters, numbers, dots, underscores, and hyphens") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects empty values", func(t *testing.T) {
		_, err := NormalizePathID("id", "   ")
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "must not be empty") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects overly long values", func(t *testing.T) {
		_, err := NormalizePathID("id", strings.Repeat("a", 65))
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "64 characters or fewer") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestSanitizeQuery(t *testing.T) {
	rules := QueryRules{
		Strings: map[string]StringRule{
			"currency": CurrencyRule(),
			"search":   SearchRule(64),
			"status":   EnumRule(16, true, "active", "past_due"),
		},
		Ints: map[string]IntRule{
			"limit": {Min: 1, Max: 100},
			"page":  {Min: 1, Max: 100000},
		},
	}

	t.Run("normalizes valid strings and integers", func(t *testing.T) {
		values, err := url.ParseQuery("status=%20ACTIVE%20&currency=ngn&search=Pro%20Plan&page=%EF%BC%92&limit=10")
		if err != nil {
			t.Fatalf("ParseQuery: %v", err)
		}

		got, err := SanitizeQuery(values, rules)
		if err != nil {
			t.Fatalf("SanitizeQuery returned error: %v", err)
		}

		if got.Strings["status"] != "active" {
			t.Fatalf("status = %q, want %q", got.Strings["status"], "active")
		}
		if got.Strings["currency"] != "NGN" {
			t.Fatalf("currency = %q, want %q", got.Strings["currency"], "NGN")
		}
		if got.Strings["search"] != "Pro Plan" {
			t.Fatalf("search = %q, want %q", got.Strings["search"], "Pro Plan")
		}
		if got.Ints["page"] != 2 {
			t.Fatalf("page = %d, want 2", got.Ints["page"])
		}
		if got.Ints["limit"] != 10 {
			t.Fatalf("limit = %d, want 10", got.Ints["limit"])
		}
	})

	t.Run("rejects unsupported parameters", func(t *testing.T) {
		_, err := SanitizeQuery(url.Values{"debug": {"true"}}, rules)
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "unsupported parameter") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects duplicate parameters", func(t *testing.T) {
		_, err := SanitizeQuery(url.Values{"limit": {"1", "2"}}, rules)
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "exactly once") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects malformed numeric values", func(t *testing.T) {
		_, err := SanitizeQuery(url.Values{"limit": {"1e2"}}, rules)
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "base-10 integer") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects overflow numeric values", func(t *testing.T) {
		_, err := SanitizeQuery(url.Values{"page": {"999999999999999999999999"}}, rules)
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "valid integer") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects encoded payload tricks", func(t *testing.T) {
		values, err := url.ParseQuery("search=%3Cscript%3E")
		if err != nil {
			t.Fatalf("ParseQuery: %v", err)
		}

		_, err = SanitizeQuery(values, rules)
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "invalid characters") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects out of range numeric values", func(t *testing.T) {
		_, err := SanitizeQuery(url.Values{"limit": {"101"}}, rules)
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "between 1 and 100") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects invalid utf8", func(t *testing.T) {
		_, err := SanitizeQuery(url.Values{"search": {string([]byte{0xff})}}, rules)
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "valid UTF-8") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects unsupported enum values", func(t *testing.T) {
		_, err := SanitizeQuery(url.Values{"status": {"paused"}}, rules)
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "unsupported value") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects empty integer values", func(t *testing.T) {
		_, err := SanitizeQuery(url.Values{"page": {"   "}}, rules)
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "must not be empty") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestIdentifierRule(t *testing.T) {
	rule := IdentifierRule(64)
	if rule.MaxLen != 64 {
		t.Fatalf("MaxLen = %d, want 64", rule.MaxLen)
	}
	if rule.Pattern == nil {
		t.Fatal("expected identifier pattern to be set")
	}
}

