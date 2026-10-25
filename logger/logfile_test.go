package logger

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode() & 0777
}

func useLogDir(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("LOG_DIR", dir)
	prev := log.Writer()
	t.Cleanup(func() {
		log.SetOutput(prev)
		aiInferenceLogger = nil
	})
}

func TestInitLogging_MissingFilesCreatedWorldReadable(t *testing.T) {
	dir := t.TempDir()
	useLogDir(t, dir)

	lj := InitLogging()
	defer lj.Close()

	for _, name := range []string{"app.log", "ai_inference.log"} {
		if got := modeOf(t, filepath.Join(dir, name)); got != 0644 {
			t.Errorf("%s mode = %o, want 644", name, got)
		}
	}
}

func TestInitLogging_ExistingPrivateFilesBecomeWorldReadable(t *testing.T) {
	dir := t.TempDir()
	useLogDir(t, dir)
	for _, name := range []string{"app.log", "ai_inference.log"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("old\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	lj := InitLogging()
	defer lj.Close()

	for _, name := range []string{"app.log", "ai_inference.log"} {
		if got := modeOf(t, filepath.Join(dir, name)); got != 0644 {
			t.Errorf("%s mode = %o, want 644", name, got)
		}
	}
}

func TestInitLogging_RotatedActiveFileStaysWorldReadable(t *testing.T) {
	dir := t.TempDir()
	useLogDir(t, dir)

	lj := InitLogging()
	defer lj.Close()
	if err := lj.Rotate(); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	log.SetOutput(io.Discard)
	if _, err := lj.Write([]byte("after rotate\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	if got := modeOf(t, filepath.Join(dir, "app.log")); got != 0644 {
		t.Errorf("app.log mode after rotate = %o, want 644", got)
	}

	// Why: lumberjack compresses the backup asynchronously; wait so TempDir cleanup does not race it.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		gz, _ := filepath.Glob(filepath.Join(dir, "app-*.log.gz"))
		raw, _ := filepath.Glob(filepath.Join(dir, "app-*.log"))
		if len(gz) == 1 && len(raw) == 0 {
			if got := modeOf(t, gz[0]); got != 0644 {
				t.Errorf("compressed backup mode = %o, want 644", got)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("compressed backup did not appear")
}

func TestRotateReadable_RecreatesDeletedFileWorldReadable(t *testing.T) {
	dir := t.TempDir()
	useLogDir(t, dir)

	lj := InitLogging()
	defer lj.Close()
	path := filepath.Join(dir, "app.log")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	if err := rotateReadable(lj); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	if got := modeOf(t, path); got != 0644 {
		t.Errorf("app.log mode = %o, want 644", got)
	}
}
