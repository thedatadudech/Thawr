package api

import "context"

// Status is the JSON body of GET /api/v1/status.
type Status struct {
	Version          string `json:"version"`
	UptimeSeconds    int64  `json:"uptime_seconds"`
	PeerCount        int    `json:"peer_count"`
	NetmapGeneration int64  `json:"netmap_generation"`
	TLSFingerprint   string `json:"tls_fingerprint"`
	HubPublicKey     string `json:"hub_public_key"`
}

// StatusSource provides the live status; the server implements it.
type StatusSource interface {
	Status(ctx context.Context) (Status, error)
}
