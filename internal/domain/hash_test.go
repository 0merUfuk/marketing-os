package domain

import "testing"

func TestDedupeKeyUsesStableProductWorkflowRepositoryAndReleaseIDs(t *testing.T) {
	t.Parallel()
	a := ReleaseDedupeKey("alpha", "release-to-marketing", 1001, 42)
	b := ReleaseDedupeKey("alpha", "release-to-marketing", 1001, 42)
	if a != b || a == "" {
		t.Fatalf("unstable dedupe key: %q != %q", a, b)
	}
	if a == ReleaseDedupeKey("beta", "release-to-marketing", 1001, 42) {
		t.Fatal("dedupe key collided across products")
	}
	if a == ReleaseDedupeKey("alpha", "release-to-marketing", 1002, 42) {
		t.Fatal("dedupe key collided across repositories")
	}
}

func TestAssetContentHashIncludesChannelSubjectAndContent(t *testing.T) {
	t.Parallel()
	base := AssetContentHash("email", "Subject", "Body")
	for _, changed := range []string{
		AssetContentHash("linkedin", "Subject", "Body"),
		AssetContentHash("email", "Other", "Body"),
		AssetContentHash("email", "Subject", "Other"),
	} {
		if changed == base {
			t.Fatal("asset hash ignored a meaningful field")
		}
	}
}
