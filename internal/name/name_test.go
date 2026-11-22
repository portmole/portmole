package name

import (
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	for in, want := range map[string]string{
		"My Shop":                        "my-shop",
		"myshop":                         "myshop",
		"--api_v2..":                     "api-v2",
		"Mağaza":                         "ma-aza",
		"___":                            "",
		"":                               "",
		"web (staging)":                  "web-staging",
		strings.Repeat("a", 29) + "-bcd": strings.Repeat("a", 29),
	} {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFromDir(t *testing.T) {
	if got := FromDir(`/home/dev/My Shop`); got != "my-shop" {
		t.Errorf("FromDir = %q, want my-shop", got)
	}
	a, b := FromDir("/"), FromDir("/")
	if len(a) != 8 || a == b || Clean(a) != a {
		t.Errorf("unusable folder gave %q and %q, want two different valid 8-character names", a, b)
	}
}
