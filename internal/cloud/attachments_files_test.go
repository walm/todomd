package cloud

import "testing"

func TestCleanFileName(t *testing.T) {
	for in, want := range map[string]string{
		"plan.md":              "plan.md",
		"My Plan.MD":           "My-Plan.md",
		"report (final).pdf":   "report-final.pdf",
		"a.tar.gz":             "a.tar.gz",
		"Makefile":             "Makefile",
		".md":                  "file.md",
		"notes.verylongextens": "notes",
		"ünï.txt":              "u-ni.txt",
	} {
		if got := CleanFileName(in); got != want {
			t.Errorf("CleanFileName(%q) = %q, want %q", in, got, want)
		}
		if again := CleanFileName(CleanFileName(in)); again != want {
			t.Errorf("not idempotent for %q: %q", in, again)
		}
	}
}

func TestIsImageBySizeAndSignature(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 10)...)
	if !IsImage(png) || IsImage([]byte("# plan")) {
		t.Error("signature decides")
	}
	if IsImage(append(png, make([]byte, MaxImageBytes)...)) {
		t.Error("an image over 5 MB is a file")
	}
}
