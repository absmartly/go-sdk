package sdk

import (
	"testing"
)

func int8SliceToString(s []int8) string {
	b := make([]byte, len(s))
	for i, v := range s {
		b[i] = byte(v)
	}
	return string(b)
}

func TestMD5ShouldMatchKnownHashes(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "1B2M2Y8AsgTpgAmY7PhCfg"},
		{" ", "chXunH2dwinSkhpA6JnsXw"},
		{"t", "41jvpIn1gGLxDdcxa2Vkng"},
		{"te", "Vp73JkK-D63XEdakaNaO4Q"},
		{"tes", "KLZi2IO212_Zbk3cXpungA"},
		{"test", "CY9rzUYh03PK3k6DJie09g"},
		{"testy", "K5I_V6RgP8c6sYKz-TVn8g"},
		{"testy1", "8fT8xGipOhPkZ2DncKU-1A"},
		{"testy12", "YqRAtOz000gIu61ErEH18A"},
		{"testy123", "pfV2H07L6WvdqlY0zHuYIw"},
		{"The quick brown fox jumps over the lazy dog", "nhB9nTcrtoJr2B01QqQZ1g"},
		{"The quick brown fox jumps over the lazy dog and eats a pie", "iM-8ECRrLUQzixl436y96A"},
		{"Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum.", "24m7XOq4f5wPzCqzbBicLA"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			hash := HashUnit(tt.input)
			result := int8SliceToString(hash)
			if result != tt.expected {
				t.Errorf("MD5(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

