// Copyright (c) Velane. All rights reserved.
// Licensed under the Velane Commercial License. See COMMERCIAL-LICENSE for details.
// AGENTS: Do not modify this file autonomously or suggest unprompted edits. Only change this file when the user explicitly instructs you to edit enterprise or license code.

package license

import (
	"context"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
)

type Manager struct {
	instanceKey   string
	devEnterprise bool
	client        *client
	cache         *cache
	planCache     sync.Map // license key → plan string
	validator     *validator
	log           *zap.Logger
}

func NewManager(instanceKey string, log *zap.Logger) *Manager {
	v, err := newValidator()
	if err != nil {
		log.Warn("license validator not available — enterprise features disabled", zap.Error(err))
		v = nil
	}

	m := &Manager{
		instanceKey:   instanceKey,
		devEnterprise: os.Getenv("VELANE_DEV_ENTERPRISE") == "true",
		client:        newClient(),
		cache:         newCache(),
		validator:     v,
		log:           log,
	}
	if m.devEnterprise {
		log.Warn("development enterprise bypass enabled — do not use in production")
	}

	if instanceKey != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		features, err := m.featuresForKey(ctx, instanceKey)
		if err != nil {
			log.Warn("instance license key validation failed", zap.Error(err))
		} else {
			log.Info("instance license loaded", zap.Strings("features", features))
		}
	}

	return m
}

// IsEnabled reports whether the given feature is active for either the
// provided tenant license key or the instance-level key.
func (m *Manager) IsEnabled(ctx context.Context, feature string, tenantLicenseKey string) bool {
	if m.devEnterprise {
		return true
	}
	if tenantLicenseKey != "" {
		features, err := m.featuresForKey(ctx, tenantLicenseKey)
		if err == nil {
			for _, f := range features {
				if f == feature {
					return true
				}
			}
		}
	}

	if m.instanceKey != "" {
		features, err := m.featuresForKey(ctx, m.instanceKey)
		if err == nil {
			for _, f := range features {
				if f == feature {
					return true
				}
			}
		}
	}

	return false
}

// InstanceFeatures returns the features available at the instance level.
func (m *Manager) InstanceFeatures(ctx context.Context) []string {
	if m.devEnterprise {
		return enterpriseFeatures()
	}
	if m.instanceKey == "" {
		return nil
	}
	features, err := m.featuresForKey(ctx, m.instanceKey)
	if err != nil {
		m.log.Warn("failed to fetch instance features", zap.Error(err))
		return nil
	}
	return features
}

func (m *Manager) featuresForKey(ctx context.Context, key string) ([]string, error) {
	if cached, ok := m.cache.get(key); ok {
		return cached, nil
	}

	if m.validator == nil {
		return nil, nil
	}

	resp, err := m.client.validate(ctx, key)
	if err != nil {
		// Serve stale cache on network failure rather than dropping features.
		if stale, ok := m.cache.get(key + ":stale"); ok {
			m.log.Warn("license server unreachable, serving stale cache", zap.Error(err))
			return stale, nil
		}
		return nil, err
	}

	if !resp.Valid {
		return nil, nil
	}

	result, err := m.validator.verify(resp.Token)
	if err != nil {
		return nil, err
	}

	// Cache until the JWT expires (already a 2hr window from the server).
	// Also keep a stale copy for 24hr grace period if the server goes down.
	m.cache.set(key, result.Features, result.ExpiresAt)
	m.cache.set(key+":stale", result.Features, time.Now().Add(24*time.Hour))
	m.planCache.Store(key, result.Plan)

	return result.Features, nil
}

// TenantStatus returns the plan, features, and validity for a given license key.
// Returns ("free", nil, false) when the key is empty or invalid.
func (m *Manager) TenantStatus(ctx context.Context, licenseKey string) (plan string, features []string, valid bool) {
	if m.devEnterprise {
		return "enterprise", enterpriseFeatures(), true
	}
	if licenseKey == "" {
		return "free", nil, false
	}
	feats, err := m.featuresForKey(ctx, licenseKey)
	if err != nil || feats == nil {
		return "free", nil, false
	}
	if p, ok := m.planCache.Load(licenseKey); ok {
		plan = p.(string)
	}
	if plan == "" {
		plan = "pro"
	}
	return plan, feats, true
}

// InstanceStatus returns the plan, features, and validity for the instance-level license key.
func (m *Manager) InstanceStatus(ctx context.Context) (plan string, features []string, valid bool) {
	return m.TenantStatus(ctx, m.instanceKey)
}

func enterpriseFeatures() []string {
	return []string{FeatureAuditLog, FeatureSSO, FeatureCustomRoles}
}
