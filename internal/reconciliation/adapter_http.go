package reconciliation

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"

    "stellarbill-backend/internal/httpclient"
)

// HTTPAdapter fetches snapshots from a configured HTTP endpoint.
type HTTPAdapter struct {
    Client *httpclient.Client
    URL    string
    // Optional Authorization header value (e.g., Bearer <token>)
    AuthHeader string
}

// NewHTTPAdapter creates an adapter that will GET snapshots from url.
func NewHTTPAdapter(url string, authHeader string) *HTTPAdapter {
    return &HTTPAdapter{Client: httpclient.NewClient(), URL: url, AuthHeader: authHeader}
}

// FetchSnapshots implements Adapter.
func (h *HTTPAdapter) FetchSnapshots(ctx context.Context) ([]Snapshot, error) {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.URL, nil)
    if err != nil {
        return nil, err
    }
    if h.AuthHeader != "" {
        req.Header.Set("Authorization", h.AuthHeader)
    }
    req.Header.Set("Accept", "application/json")

    resp, err := h.Client.Do(req)
    if err != nil {
        return nil, err
    }
    defer func() {
        if resp.Body != nil {
            io.Copy(io.Discard, resp.Body)
            resp.Body.Close()
        }
    }()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
    }

    var snaps []Snapshot
    dec := json.NewDecoder(resp.Body)
    if err := dec.Decode(&snaps); err != nil {
        return nil, err
    }
    return snaps, nil
}
