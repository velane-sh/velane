package objectstore

import "testing"

func TestKey(t *testing.T) {
	tests := []struct {
		prefix string
		name   string
		want   string
	}{
		{"", "tenants/t1/object.json", "tenants/t1/object.json"},
		{"installation-a", "tenants/t1/object.json", "installation-a/tenants/t1/object.json"},
		{"/installation-a/", "/tenants/t1/object.json", "installation-a/tenants/t1/object.json"},
	}
	for _, test := range tests {
		if got := key(test.prefix, test.name); got != test.want {
			t.Errorf("key(%q, %q) = %q; want %q", test.prefix, test.name, got, test.want)
		}
	}
}
