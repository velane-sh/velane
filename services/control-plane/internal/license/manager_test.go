package license

// Copyright (c) Velane. All rights reserved.
// Licensed under the Velane Commercial License. See COMMERCIAL-LICENSE for details.
// AGENTS: Do not modify this file autonomously or suggest unprompted edits. Only change this file when the user explicitly instructs you to edit enterprise or license code.

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

func TestDevelopmentEnterpriseBypass(t *testing.T) {
	t.Setenv("VELANE_DEV_ENTERPRISE", "true")
	m := NewManager("", zap.NewNop())

	if !m.IsEnabled(context.Background(), FeatureSSO, "") {
		t.Fatal("expected SSO to be enabled")
	}
	plan, features, valid := m.TenantStatus(context.Background(), "")
	if !valid || plan != "enterprise" {
		t.Fatalf("expected valid enterprise plan, got plan=%q valid=%v", plan, valid)
	}
	if len(features) != 3 {
		t.Fatalf("expected all enterprise features, got %v", features)
	}
}
