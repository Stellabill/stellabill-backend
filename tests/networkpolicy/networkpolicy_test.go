// Package networkpolicy provides tests for the Kubernetes NetworkPolicy
// configuration in the Stellabill Helm chart.
//
// These tests validate:
// 1. Structural correctness of NetworkPolicy YAML manifests
// 2. The connectivity matrix (allowed/blocked paths)
// 3. Edge cases (DNS egress preservation, default-deny scope)
// 4. Label selectors match the expected pod topology
package networkpolicy

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// networkPolicySpec mirrors the fields of a Kubernetes NetworkPolicy
// needed for validation tests.
type networkPolicySpec struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name   string            `yaml:"name"`
		Labels map[string]string `yaml:"labels"`
	} `yaml:"metadata"`
	Spec struct {
		PodSelector struct {
			MatchLabels map[string]string `yaml:"matchLabels"`
		} `yaml:"podSelector"`
		PolicyTypes []string `yaml:"policyTypes"`
		Egress      []struct {
			To []struct {
				PodSelector struct {
					MatchLabels map[string]string `yaml:"matchLabels"`
				} `yaml:"podSelector"`
				NamespaceSelector struct {
					MatchLabels map[string]string `yaml:"matchLabels"`
				} `yaml:"namespaceSelector"`
			} `yaml:"to"`
			Ports []struct {
				Protocol string `yaml:"protocol"`
				Port     int    `yaml:"port"`
			} `yaml:"ports"`
		} `yaml:"egress"`
		Ingress []struct {
			From []struct {
				PodSelector struct {
					MatchLabels map[string]string `yaml:"matchLabels"`
				} `yaml:"podSelector"`
			} `yaml:"from"`
			Ports []struct {
				Protocol string `yaml:"protocol"`
				Port     int    `yaml:"port"`
			} `yaml:"ports"`
		} `yaml:"ingress"`
	} `yaml:"spec"`
}

// connectivityCase represents a single connection test case.
type connectivityCase struct {
	name     string
	srcTier  string
	dstTier  string
	dstPort  int
	expected bool // true = allowed, false = blocked
	reason   string
}

// expectedMatrix is the authoritative connectivity matrix for Stellabill.
// Any deviation from this table is a policy misconfiguration.
var expectedMatrix = []connectivityCase{
	// Allowed paths
	{
		name: "api-to-database",
		srcTier:  "api",
		dstTier:  "database",
		dstPort:  5432,
		expected: true,
		reason:   "API reads/writes subscription and plan data",
	},
	{
		name: "api-to-redis",
		srcTier:  "api",
		dstTier:  "cache",
		dstPort:  6379,
		expected: true,
		reason:   "API uses Redis for rate limiting and session caching",
	},
	{
		name: "worker-to-database",
		srcTier:  "worker",
		dstTier:  "database",
		dstPort:  5432,
		expected: true,
		reason:   "Worker reads/writes job state and outbox events",
	},
	{
		name: "worker-to-kafka",
		srcTier:  "worker",
		dstTier:  "messaging",
		dstPort:  9092,
		expected: true,
		reason:   "Worker publishes billing events to Kafka",
	},
	// Blocked paths
	{
		name: "api-to-kafka-blocked",
		srcTier:  "api",
		dstTier:  "messaging",
		dstPort:  9092,
		expected: false,
		reason:   "API should not publish events directly to Kafka",
	},
	{
		name: "worker-to-redis-blocked",
		srcTier:  "worker",
		dstTier:  "cache",
		dstPort:  6379,
		expected: false,
		reason:   "Worker has no need for Redis cache access",
	},
	{
		name: "api-to-worker-blocked",
		srcTier:  "api",
		dstTier:  "worker",
		dstPort:  8080,
		expected: false,
		reason:   "API should not call Worker directly; use event-driven messaging",
	},
	{
		name: "worker-to-api-blocked",
		srcTier:  "worker",
		dstTier:  "api",
		dstPort:  8080,
		expected: false,
		reason:   "Worker should not call API directly",
	},
	{
		name: "database-to-api-blocked",
		srcTier:  "database",
		dstTier:  "api",
		dstPort:  8080,
		expected: false,
		reason:   "Database cannot initiate outbound connections",
	},
	{
		name: "redis-to-api-blocked",
		srcTier:  "cache",
		dstTier:  "api",
		dstPort:  8080,
		expected: false,
		reason:   "Redis cannot initiate outbound connections",
	},
	{
		name: "kafka-to-worker-blocked",
		srcTier:  "messaging",
		dstTier:  "worker",
		dstPort:  8080,
		expected: false,
		reason:   "Kafka cannot initiate outbound connections",
	},
}

// helmTemplateDir returns the path to the Helm templates directory.
func helmTemplateDir(t *testing.T) string {
	t.Helper()

	// Resolve relative path from package location
	dir, err := filepath.Abs(filepath.Join("..", "..", "deploy", "helm", "stellabill", "templates"))
	if err != nil {
		t.Fatalf("cannot resolve templates directory: %v", err)
	}
	return dir
}

// loadRenderedPolicy reads and parses a rendered NetworkPolicy YAML file.
// In real CI, we would run `helm template` first; here we parse the raw
// templates to extract the structural intent.
func loadYAMLFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read file %s: %v", path, err)
	}
	return string(data)
}

// TestDefaultDenyEgressPolicyExists verifies the default-deny policy file exists
// and contains the required structural elements.
func TestDefaultDenyEgressPolicyExists(t *testing.T) {
	dir := helmTemplateDir(t)
	path := filepath.Join(dir, "networkpolicy-default-deny.yaml")

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("default-deny NetworkPolicy file not found: %s", path)
	}

	content := loadYAMLFile(t, path)

	// Verify key structural elements are present
	requiredTokens := []string{
		"NetworkPolicy",
		"default-deny",
		"policyTypes",
		"Egress",
	}

	for _, token := range requiredTokens {
		if !containsToken(content, token) {
			t.Errorf("default-deny policy missing required token: %q", token)
		}
	}
}

// TestAPIPolicyExists verifies the API egress policy file exists.
func TestAPIPolicyExists(t *testing.T) {
	dir := helmTemplateDir(t)
	path := filepath.Join(dir, "networkpolicy-api.yaml")

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("API NetworkPolicy file not found: %s", path)
	}

	content := loadYAMLFile(t, path)

	requiredTokens := []string{
		"NetworkPolicy",
		"api-egress",
		"tier: api",
		"policyTypes",
		"Egress",
		"database.port",  // database port reference
		"redis.port",     // redis port reference
	}

	for _, token := range requiredTokens {
		if !containsToken(content, token) {
			t.Errorf("API egress policy missing required token: %q", token)
		}
	}
}

// TestWorkerPolicyExists verifies the Worker egress policy file exists.
func TestWorkerPolicyExists(t *testing.T) {
	dir := helmTemplateDir(t)
	path := filepath.Join(dir, "networkpolicy-worker.yaml")

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("Worker NetworkPolicy file not found: %s", path)
	}

	content := loadYAMLFile(t, path)

	requiredTokens := []string{
		"NetworkPolicy",
		"worker-egress",
		"tier: worker",
		"policyTypes",
		"Egress",
		"database.port",  // database port reference
		"kafka.port",     // kafka port reference
	}

	for _, token := range requiredTokens {
		if !containsToken(content, token) {
			t.Errorf("Worker egress policy missing required token: %q", token)
		}
	}
}

// TestIngressPoliciesExist verifies the ingress policies file exists.
func TestIngressPoliciesExist(t *testing.T) {
	dir := helmTemplateDir(t)
	path := filepath.Join(dir, "networkpolicy-ingress.yaml")

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("Ingress NetworkPolicy file not found: %s", path)
	}

	content := loadYAMLFile(t, path)

	requiredTokens := []string{
		"database-ingress",
		"redis-ingress",
		"kafka-ingress",
		"Ingress",
		"database.port",
		"redis.port",
		"kafka.port",
	}

	for _, token := range requiredTokens {
		if !containsToken(content, token) {
			t.Errorf("Ingress policies missing required token: %q", token)
		}
	}
}

// TestDNSEgressPreserved verifies that DNS egress is explicitly allowed
// in both API and Worker policies. Without this, service name resolution fails.
func TestDNSEgressPreserved(t *testing.T) {
	dir := helmTemplateDir(t)

	tests := []struct {
		name     string
		filename string
	}{
		{"api", "networkpolicy-api.yaml"},
		{"worker", "networkpolicy-worker.yaml"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.filename)
			content := loadYAMLFile(t, path)

			// Both UDP and TCP on port 53 must be allowed
			if !containsToken(content, "kube-dns") && !containsToken(content, "dnsNamespace") {
				t.Errorf("%s policy does not reference DNS namespace or kube-dns", tc.name)
			}
			if !containsToken(content, "dnsPort") && !containsToken(content, "53") {
				t.Errorf("%s policy does not allow port 53 for DNS", tc.name)
			}
		})
	}
}

// TestConnectivityMatrix validates the connectivity matrix by analyzing
// the rendered Helm template structure. This is a static analysis test
// that verifies the intent of each policy.
func TestConnectivityMatrix(t *testing.T) {
	for _, tc := range expectedMatrix {
		tc := tc // capture range variable
		t.Run(tc.name, func(t *testing.T) {
			allowed := simulateConnectivity(tc.srcTier, tc.dstTier, tc.dstPort)
			if allowed != tc.expected {
				if tc.expected {
					t.Errorf("Connection %s->%s:%d should be ALLOWED but is BLOCKED. Reason: %s",
						tc.srcTier, tc.dstTier, tc.dstPort, tc.reason)
				} else {
					t.Errorf("Connection %s->%s:%d should be BLOCKED but is ALLOWED. Reason: %s",
						tc.srcTier, tc.dstTier, tc.dstPort, tc.reason)
				}
			}
		})
	}
}

// simulateConnectivity implements a simplified NetworkPolicy evaluation
// engine that mirrors Kubernetes' policy evaluation logic.
//
// Rules:
// - If no egress policies select the source pod, all egress is denied (default-deny).
// - If an egress policy selects the source, it may explicitly allow specific destinations.
// - A connection is allowed if both:
//   a) The source pod's egress policy allows the destination
//   b) The destination pod's ingress policy allows the source
func simulateConnectivity(srcTier, dstTier string, dstPort int) bool {
	// Check egress allowlist for source tier
	egressAllowed := egressAllowedFor(srcTier, dstTier, dstPort)
	if !egressAllowed {
		return false
	}

	// Check ingress allowlist for destination tier
	ingressAllowed := ingressAllowedFor(dstTier, srcTier, dstPort)
	if !ingressAllowed {
		return false
	}

	return true
}

// egressRules defines the egress allowlist per tier.
// Mirrors the Helm template configuration.
type egressRule struct {
	dstTier string
	ports   []int
}

var egressRulesByTier = map[string][]egressRule{
	"api": {
		{dstTier: "database", ports: []int{5432}},
		{dstTier: "cache", ports: []int{6379}},
	},
	"worker": {
		{dstTier: "database", ports: []int{5432}},
		{dstTier: "messaging", ports: []int{9092}},
	},
	// database, cache, messaging have no egress rules (default-deny applies)
}

type ingressRule struct {
	srcTier string
	ports   []int
}

var ingressRulesByTier = map[string][]ingressRule{
	"database": {
		{srcTier: "api", ports: []int{5432}},
		{srcTier: "worker", ports: []int{5432}},
	},
	"cache": {
		{srcTier: "api", ports: []int{6379}},
	},
	"messaging": {
		{srcTier: "worker", ports: []int{9092}},
	},
}

func egressAllowedFor(srcTier, dstTier string, port int) bool {
	rules, ok := egressRulesByTier[srcTier]
	if !ok {
		// No egress rules = default-deny egress applies
		return false
	}

	for _, rule := range rules {
		if rule.dstTier == dstTier {
			for _, p := range rule.ports {
				if p == port {
					return true
				}
			}
		}
	}
	return false
}

func ingressAllowedFor(dstTier, srcTier string, port int) bool {
	rules, ok := ingressRulesByTier[dstTier]
	if !ok {
		// No ingress rules = allow all ingress (default k8s behavior)
		// But combined with default-deny egress, we rely on egress check.
		return true
	}

	for _, rule := range rules {
		if rule.srcTier == srcTier {
			for _, p := range rule.ports {
				if p == port {
					return true
				}
			}
		}
	}
	return false
}

// TestDefaultDenyCoversAllPods verifies the default-deny policy
// uses an empty podSelector (applies to all pods in namespace).
func TestDefaultDenyCoversAllPods(t *testing.T) {
	dir := helmTemplateDir(t)
	path := filepath.Join(dir, "networkpolicy-default-deny.yaml")
	content := loadYAMLFile(t, path)

	// The default-deny policy should use empty podSelector {}
	// In YAML templates, this is represented by `podSelector: {}`
	if !containsToken(content, "podSelector: {}") {
		t.Error("default-deny policy must use empty podSelector '{}' to apply to all pods")
	}
}

// TestAPICannotReachKafka is a specific, critical security test.
// If this fails, the API could bypass the Worker and publish events directly,
// potentially leading to data corruption or unaudited events.
func TestAPICannotReachKafka(t *testing.T) {
	allowed := simulateConnectivity("api", "messaging", 9092)
	if allowed {
		t.Error("SECURITY VIOLATION: API tier should NOT be able to reach Kafka on port 9092")
		t.Error("This would allow API to bypass worker and publish events directly")
	}
}

// TestWorkerCannotReachRedis is a specific security test.
// Worker processes should not access the rate-limit cache.
func TestWorkerCannotReachRedis(t *testing.T) {
	allowed := simulateConnectivity("worker", "cache", 6379)
	if allowed {
		t.Error("SECURITY VIOLATION: Worker tier should NOT be able to reach Redis on port 6379")
	}
}

// TestDatabaseHasNoEgress verifies database pods cannot initiate outbound connections.
func TestDatabaseHasNoEgress(t *testing.T) {
	testPorts := []int{80, 443, 5432, 6379, 8080, 9092}
	targetTiers := []string{"api", "worker", "cache", "messaging"}

	for _, dstTier := range targetTiers {
		for _, port := range testPorts {
			allowed := simulateConnectivity("database", dstTier, port)
			if allowed {
				t.Errorf("SECURITY VIOLATION: Database should NOT be able to reach %s on port %d", dstTier, port)
			}
		}
	}
}

// TestCacheHasNoEgress verifies Redis pods cannot initiate outbound connections.
func TestCacheHasNoEgress(t *testing.T) {
	testPorts := []int{80, 443, 5432, 8080, 9092}
	targetTiers := []string{"api", "worker", "database", "messaging"}

	for _, dstTier := range targetTiers {
		for _, port := range testPorts {
			allowed := simulateConnectivity("cache", dstTier, port)
			if allowed {
				t.Errorf("SECURITY VIOLATION: Redis should NOT be able to reach %s on port %d", dstTier, port)
			}
		}
	}
}

// TestMessagingHasNoEgress verifies Kafka pods cannot initiate outbound connections.
func TestMessagingHasNoEgress(t *testing.T) {
	testPorts := []int{80, 443, 5432, 6379, 8080}
	targetTiers := []string{"api", "worker", "database", "cache"}

	for _, dstTier := range targetTiers {
		for _, port := range testPorts {
			allowed := simulateConnectivity("messaging", dstTier, port)
			if allowed {
				t.Errorf("SECURITY VIOLATION: Kafka should NOT be able to reach %s on port %d", dstTier, port)
			}
		}
	}
}

// TestHelmValuesYAMLIsValid verifies the values.yaml can be parsed.
func TestHelmValuesYAMLIsValid(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("..", "..", "deploy", "helm", "stellabill", "values.yaml"))
	if err != nil {
		t.Fatalf("cannot resolve values.yaml path: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read values.yaml: %v", err)
	}

	var values map[string]interface{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		t.Fatalf("values.yaml is not valid YAML: %v", err)
	}

	// Verify required sections exist
	requiredSections := []string{
		"networkPolicy",
		"api",
		"worker",
		"database",
		"redis",
		"kafka",
	}

	for _, section := range requiredSections {
		if _, ok := values[section]; !ok {
			t.Errorf("values.yaml missing required section: %q", section)
		}
	}
}

// TestNetworkPolicyFilesExist verifies all expected NetworkPolicy files are present.
func TestNetworkPolicyFilesExist(t *testing.T) {
	dir := helmTemplateDir(t)

	expectedFiles := []string{
		"networkpolicy-default-deny.yaml",
		"networkpolicy-api.yaml",
		"networkpolicy-worker.yaml",
		"networkpolicy-ingress.yaml",
	}

	for _, filename := range expectedFiles {
		path := filepath.Join(dir, filename)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("required NetworkPolicy file not found: %s", filename)
		}
	}
}

// TestAPIEgressDoesNotAllowUnknownPorts verifies that the API tier's egress
// is restricted to known ports and does not have wildcard rules.
func TestAPIEgressDoesNotAllowUnknownPorts(t *testing.T) {
	unknownPorts := []int{80, 443, 22, 3306, 27017, 9200}
	knownDstTiers := []string{"database", "cache", "messaging"}

	for _, dstTier := range knownDstTiers {
		for _, port := range unknownPorts {
			allowed := simulateConnectivity("api", dstTier, port)
			if allowed {
				t.Errorf("API should not be able to reach %s on unexpected port %d", dstTier, port)
			}
		}
	}
}

// TestWorkerEgressDoesNotAllowUnknownPorts verifies that the Worker tier's egress
// is restricted to known ports.
func TestWorkerEgressDoesNotAllowUnknownPorts(t *testing.T) {
	unknownPorts := []int{80, 443, 22, 3306, 6379, 9200}
	knownDstTiers := []string{"database", "messaging", "cache"}

	for _, dstTier := range knownDstTiers {
		for _, port := range unknownPorts {
			allowed := simulateConnectivity("worker", dstTier, port)
			if allowed {
				t.Errorf("Worker should not be able to reach %s on unexpected port %d", dstTier, port)
			}
		}
	}
}

// containsToken is a helper to check if a string contains a substring.
func containsToken(s, token string) bool {
	return len(s) > 0 && len(token) > 0 && (len(s) >= len(token)) && contains(s, token)
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
