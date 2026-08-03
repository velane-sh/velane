package nango

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestListRecords_QueryAndNormalization(t *testing.T) {
	c := New("http://nango.test", "secret")
	c.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		q := r.URL.Query()
		for key, want := range map[string]string{"connection_id": "nc-1", "provider_config_key": "cfg", "model": "Case", "modified_after": "2026-08-03T10:00:00Z", "cursor": "page-2", "limit": "100"} {
			if q.Get(key) != want {
				t.Errorf("%s=%q, want %q", key, q.Get(key), want)
			}
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("missing authorization")
		}
		return response(200, `{"records":[{"id":"1","_nango_metadata":{"last_action":"ADDED"}},{"id":"2","_nango_metadata":{"last_action":"DELETED"}},{"id":"3","name":"changed"}],"next_cursor":"page-3"}`), nil
	})}
	page, err := c.ListRecords(context.Background(), "nc-1", "cfg", "Case", "2026-08-03T10:00:00Z", "page-2")
	if err != nil {
		t.Fatal(err)
	}
	if page.NextCursor != "page-3" {
		t.Errorf("cursor=%q", page.NextCursor)
	}
	want := []string{"added", "deleted", "updated"}
	for i, action := range want {
		if page.Records[i].Action != action {
			t.Errorf("record %d action=%q, want %q", i, page.Records[i].Action, action)
		}
	}
}

func TestListSyncModels_DeduplicatesInOrder(t *testing.T) {
	c := New("http://nango.test", "secret")
	c.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("provider_config_key") != "salesforce tenant" {
			t.Errorf("provider key not decoded")
		}
		return response(200, `{"data":[{"models":["Case","Contact"]},{"models":["Case","Account"]}]}`), nil
	})}
	got, err := c.ListSyncModels(context.Background(), "salesforce tenant")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "Case,Contact,Account" {
		t.Fatalf("models=%v", got)
	}
}

func TestListSyncModels_Unavailable(t *testing.T) {
	c := New("http://nango.test", "")
	c.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(404, `{}`), nil })}
	if _, err := c.ListSyncModels(context.Background(), "cfg"); err == nil {
		t.Fatal("expected error")
	}
}
