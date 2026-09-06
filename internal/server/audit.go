package server

import (
	"context"
	"time"
)

// auditPruneInterval is how often old audit entries are removed.
const auditPruneInterval = 24 * time.Hour

// pruneAudit removes audit entries older than audit.retention_days at
// start and once a day; a retention of 0 keeps everything.
func (s *Server) pruneAudit(ctx context.Context) {
	days := s.cfg.Audit.RetentionDays
	if days <= 0 {
		return
	}
	interval := auditPruneInterval
	if s.deps.AuditPruneInterval > 0 {
		interval = s.deps.AuditPruneInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		cutoff := s.deps.Now().Add(-time.Duration(days) * 24 * time.Hour)
		n, err := s.st.Audit().Prune(ctx, cutoff)
		switch {
		case err != nil && ctx.Err() == nil:
			s.log.Error("audit: prune", "err", err)
		case n > 0:
			s.log.Info("audit: pruned old entries", "removed", n, "older_than", cutoff.UTC().Format(time.RFC3339))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
