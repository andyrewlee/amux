package diff

import "testing"

func TestParseDiff(t *testing.T) {
	input := `diff --git a/foo.txt b/foo.txt
index 111..222 100644
--- a/foo.txt
+++ b/foo.txt
@@ -1,2 +1,2 @@
-hello
+hello world
 context`
	files := Parse(input)
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Path != "foo.txt" {
		t.Fatalf("expected foo.txt, got %q", files[0].Path)
	}
	if files[0].Added != 1 || files[0].Deleted != 1 {
		t.Fatalf("expected counts 1/1, got +%d -%d", files[0].Added, files[0].Deleted)
	}
}
