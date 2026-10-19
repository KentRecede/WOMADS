// Package audit records critical actions (account changes, deletions, ...)
// in the audit_logs table, as required by NFR 5.3.6 of the paper.
package audit

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"womads/internal/auth"
	"womads/internal/models"
)

// Record writes one audit entry for the current request's caller.
// A failure to audit is logged but never fails the request that already succeeded.
func Record(r *http.Request, db *gorm.DB, action, entity, entityID string) {
	entry := models.AuditLog{Action: action, Entity: entity}
	if p := auth.PrincipalFrom(r.Context()); p != nil {
		entry.ActorID = &p.ID
	}
	if entityID != "" {
		entry.EntityID = &entityID
	}
	if ip := clientIP(r); ip != "" {
		entry.IP = &ip
	}

	// The request context is cancelled as soon as the response is sent.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second)
	defer cancel()
	if err := db.WithContext(ctx).Create(&entry).Error; err != nil {
		slog.Error("audit write failed", "err", err, "action", action, "entity", entity)
	}
}

// clientIP trusts X-Forwarded-For because in production only the gateway can
// reach this service, and the gateway overwrites that header itself.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		if ip := net.ParseIP(strings.TrimSpace(first)); ip != nil {
			return ip.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	return host
}
