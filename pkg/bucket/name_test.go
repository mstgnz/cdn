package bucket

import "testing"

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		// Names that exist in production today must stay valid.
		{"alcshare", true},
		{"assistants", true},
		{"atc-yazilim", true},
		{"dos", true},
		{"memolax", true},
		{"sahakolay", true},
		{"sovtajyeri", true},
		{"tedarik", true},
		{"tramer", true},

		{"", false},
		{"ab", false},                     // shorter than 3
		{"Uppercase", false},              // uppercase
		{"with_underscore", false},        // underscore
		{"with space", false},             // space
		{"-leading-hyphen", false},        // must start alphanumeric
		{"trailing-hyphen-", false},       // must end alphanumeric
		{"..", false},                     // traversal-looking
		{"with.dot", false},               // dots deliberately rejected
		{"bucket:secret", false},          // would break token parsing
		{" padded", false},                // whitespace is not trimmed away
		{"padded ", false},                //
		{"türkçe", false},                 // non-ASCII
		{string(make([]byte, 64)), false}, // control bytes / too long
	}

	for _, c := range cases {
		err := Validate(c.name)
		if c.ok && err != nil {
			t.Errorf("Validate(%q) = %v, want nil", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("Validate(%q) = nil, want error", c.name)
		}
	}
}

func TestValidateLengthBoundaries(t *testing.T) {
	mk := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = 'a'
		}
		return string(b)
	}

	if err := Validate(mk(3)); err != nil {
		t.Errorf("3-character name rejected: %v", err)
	}
	if err := Validate(mk(63)); err != nil {
		t.Errorf("63-character name rejected: %v", err)
	}
	if err := Validate(mk(64)); err == nil {
		t.Error("64-character name accepted, want rejection")
	}
}
