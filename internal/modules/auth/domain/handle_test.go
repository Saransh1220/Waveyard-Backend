package domain

import "testing"

func TestNormalizeAndValidateHandle(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"@Blaze", "blaze", false},
		{"  prod-blaze_01 ", "prod-blaze_01", false},
		{"ab", "ab", true},
		{"-bad", "-bad", true},
		{"has space", "has space", true},
		{"admin", "admin", true},
		{"this-handle-is-way-too-long-for-us", "this-handle-is-way-too-long-for-us", true},
	}
	for _, c := range cases {
		got := NormalizeHandle(c.in)
		if got != c.want {
			t.Fatalf("NormalizeHandle(%q) = %q, want %q", c.in, got, c.want)
		}
		if err := ValidateHandle(got); (err != nil) != c.wantErr {
			t.Fatalf("ValidateHandle(%q) err = %v, wantErr %v", got, err, c.wantErr)
		}
	}
}
