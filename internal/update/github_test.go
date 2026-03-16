package update

import "testing"

func TestPickLatestTagNameSelectsHighestSemanticVersion(t *testing.T) {
	tags := []githubTag{
		{Name: "v0.1.14"},
		{Name: "v0.1.9"},
		{Name: "v0.1.15"},
		{Name: "v0.1.2"},
	}

	latest := pickLatestTagName(tags)
	if latest != "v0.1.15" {
		t.Fatalf("unexpected latest tag: got %q want %q", latest, "v0.1.15")
	}
}

func TestCompareVersion(t *testing.T) {
	if compareVersion([3]int{0, 1, 15}, [3]int{0, 1, 14}) <= 0 {
		t.Fatal("expected 0.1.15 > 0.1.14")
	}

	if compareVersion([3]int{1, 0, 0}, [3]int{1, 0, 0}) != 0 {
		t.Fatal("expected equal versions")
	}

	if compareVersion([3]int{0, 9, 0}, [3]int{1, 0, 0}) >= 0 {
		t.Fatal("expected 0.9.0 < 1.0.0")
	}
}
