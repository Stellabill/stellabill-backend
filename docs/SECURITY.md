---

## 🔐 Authentication Cache & Key Rotation (Issue #103)

### 1. Architecture Overview
To minimize latency and reduce external dependencies, Stellabill-backend caches **JWKS (JSON Web Key Sets)** from the Identity Provider. This implementation replaces static secret validation with a dynamic, rotation-aware system.

### 2. Core Security Semantics
The cache is designed with a **"Refresh-on-Error"** strategy to ensure high availability during key rotations.

| Feature | Implementation | Purpose |
| :--- | :--- | :--- |
| **Bounded TTL** | 1 Hour (Default) | Limits the window of vulnerability if a key is compromised. |
| **Stampede Protection** | 1-Minute Refresh Limit | Prevents backend from flooding the IDP if multiple requests hit an expired cache. |
| **On-Demand Refresh** | Key ID (`kid`) Lookup | If a token contains an unknown `kid`, the cache bypasses TTL to fetch new keys immediately. |
| **Resilience Fallback** | Stale-on-Error | If the IDP is down, the system continues to use cached keys rather than failing all auth requests. |

### 3. Monitoring & Metrics
As required by Issue #103, the following metrics are tracked internally:
* **Cache Hits**: Successfully validated tokens using the in-memory set.
* **Cache Misses**: Requests that triggered a TTL-based refresh.
* **Refresh Failures**: Occurrences where the IDP was unreachable or returned invalid JWKS data.
* **Rotation Events**: Forced refreshes triggered by an unknown `kid`.

### 4. Technical Configuration
The cache is initialized in the server entry point (`main.go`) and injected into the Gin middleware.

### 5. Verification & Testing
To maintain compliance with the **95% coverage** mandate:
* **Unit Tests**: Located in `internal/auth/jwks_cache_test.go`.
* **Edge Cases Covered**: Rotation, Cache Stampede, and IDP Failure Fallback.
