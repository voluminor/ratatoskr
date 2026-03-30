package sigils

import "testing"

// // // // // // // // // //

func TestValidateName_valid(t *testing.T) {
	valid := []string{
		"abc",                              // min length 3
		"abcdefghijklmnopqrstuvwxyz012345", // 32 chars, max
		"my.sigil",
		"my-sigil",
		"my_sigil",
		"a.b",
		"123",
		"a-1.2_3",
	}
	for _, name := range valid {
		if !ValidateName(name) {
			t.Errorf("expected valid: %q", name)
		}
	}
}

func TestValidateName_invalid(t *testing.T) {
	invalid := []string{
		"",                                  // empty
		"ab",                                // too short
		"abcdefghijklmnopqrstuvwxyz0123456", // 33 chars
		"ABC",                               // uppercase
		"aBc",                               // mixed case
		"foo bar",                           // space
		"foo/bar",                           // slash
		"foo:bar",                           // colon
		"foo@bar",                           // at
		"фоо",                               // non-ASCII
		"ab!",                               // exclamation
		"ab#",                               // hash
		" abc",                              // leading space
		"abc ",                              // trailing space
		"abc\t",                             // tab
		"abc\n",                             // newline
		"a",                                 // single char
		"ab",                                // two chars
	}
	for _, name := range invalid {
		if ValidateName(name) {
			t.Errorf("expected invalid: %q", name)
		}
	}
}

// //

func BenchmarkValidateName(b *testing.B) {
	for b.Loop() {
		ValidateName("my-sigil.name_01")
	}
}
