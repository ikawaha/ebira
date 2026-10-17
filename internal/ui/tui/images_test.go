package tui

import "testing"

func TestShowImages(t *testing.T) {
	tests := []struct{ in, want string }{
		{"![red](n1.assets/red.png)", "[image: red.png]"},
		{"a ![](n1.assets/a.png) b ![x](n2.assets/b-2.webp)", "a [image: a.png] b [image: b-2.webp]"},
		{"![スクショ](n1.assets/スクショ-1.png)", "[image: スクショ-1.png]"},
		{"![s](n1.assets/%E7%94%BB%E5%83%8F.png)", "[image: 画像.png]"},
		{"![s](<n1.assets/a.png>)", "[image: a.png]"},
		// Other images and links are left to glamour.
		{"![x](https://example.com/a.png)", "![x](https://example.com/a.png)"},
		{"[link](n1.assets/a.png)", "[link](n1.assets/a.png)"},
		{"![x](../n1.assets/a.png)", "![x](../n1.assets/a.png)"},
	}
	for _, tt := range tests {
		if got := showImages(tt.in); got != tt.want {
			t.Errorf("showImages(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
