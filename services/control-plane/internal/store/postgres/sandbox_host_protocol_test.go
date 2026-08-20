package postgres

import (
	"context"
	"testing"
	"time"
)

func TestSnapshotUploadPlanFailsClosedWithoutMultipartBackend(t *testing.T) {
	store := &Store{}
	_, err := store.SandboxSnapshotUploadPlan(context.Background(), SandboxHostIdentity{HostID: "host", Incarnation: 1}, "snapshot", "allocation", 1, 1, nil)
	if err == nil || err.Error() != "snapshot multipart object storage is unavailable" {
		t.Fatalf("upload plan error = %v", err)
	}
}

func TestWatchdogGrantFailsClosedWithoutSigner(t *testing.T) {
	store := &Store{}
	if _, err := store.watchdogGrant("sandbox", "allocation", "host", 1, 1, time.Now().Add(time.Minute)); err == nil {
		t.Fatal("unsigned watchdog grant was issued")
	}
}
