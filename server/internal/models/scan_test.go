package models

import "testing"

func TestValidateSlowPath(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"/", true},
		{"/*", true},
		{"/*/*", true},
		{"relative", true},
		{"/proc", true},
		{"/sys/fs", true},
		{"/home/*/*", false},
		{"/var", false},
		{"/tmp/foo", false},
	}
	for _, tc := range cases {
		err := ValidateSlowPath(tc.in)
		if tc.wantErr && err == nil {
			t.Errorf("%q: expected error", tc.in)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%q: unexpected %v", tc.in, err)
		}
	}
}
