package webapi

import "testing"

func TestClassTokenContains(t *testing.T) {
	tests := []struct {
		val   string
		token string
		want  bool
	}{
		{"foo", "foo", true},
		{"foo bar", "foo", true},
		{"foo bar", "bar", true},
		{"foo bar baz", "bar", true},
		{"  foo  ", "foo", true},
		{"foo\tbar", "foo", true},
		{"foo\nbar", "bar", true},
		{"foo\r\nbar", "bar", true},
		{"foo", "bar", false},
		{"foo bar", "baz", false},
		{"foobar", "foo", false},
		{"foobar", "bar", false},
		{"", "foo", false},
		{"foo", "", false},
		{"class-a class-b", "class-a", true},
		{"class-a class-b", "class-b", true},
		{"class-a class-b", "class", false},
		{"a b c d e", "c", true},
		{"a b c d e", "e", true},
		{"a b c d e", "f", false},
		{"  multiple   spaces  ", "multiple", true},
		{"  multiple   spaces  ", "spaces", true},
	}
	for _, tt := range tests {
		t.Run(tt.val+"/"+tt.token, func(t *testing.T) {
			if got := classTokenContains(tt.val, tt.token); got != tt.want {
				t.Errorf("classTokenContains(%q, %q) = %v, want %v", tt.val, tt.token, got, tt.want)
			}
		})
	}
}

func BenchmarkClassTokenContains(b *testing.B) {
	val := "btn btn-primary btn-lg active disabled hover:focus modal-dialog-scrollable"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = classTokenContains(val, "active")
	}
}

func BenchmarkClassTokenContainsMiss(b *testing.B) {
	val := "btn btn-primary btn-lg active disabled hover:focus modal-dialog-scrollable"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = classTokenContains(val, "nonexistent")
	}
}
