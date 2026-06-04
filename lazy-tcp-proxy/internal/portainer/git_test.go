package portainer

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGitSHA_blob(t *testing.T) {
	// Verify against known git output: echo -n "hello" | git hash-object --stdin
	content := []byte("hello")
	got := gitSHA("blob", content)
	// "blob 5\x00hello" sha1 = b6fc4c620b67d95f953a5c1c1230aaab5db5a1b0
	want := [20]byte{
		0xb6, 0xfc, 0x4c, 0x62, 0x0b, 0x67, 0xd9, 0x5f, 0x95, 0x3a,
		0x5c, 0x1c, 0x12, 0x30, 0xaa, 0xab, 0x5d, 0xb5, 0xa1, 0xb0,
	}
	if got != want {
		t.Errorf("blob SHA mismatch:\n got  %x\n want %x", got, want)
	}
}

func TestPackTypeSize(t *testing.T) {
	cases := []struct {
		objType int
		size    int
		want    []byte
	}{
		// blob (3), size 0: (3<<4)|0 = 0x30, no more bytes
		{objBlob, 0, []byte{0x30}},
		// blob (3), size 15: (3<<4)|15 = 0x3f, no more bytes
		{objBlob, 15, []byte{0x3f}},
		// blob (3), size 16: low4=0, remaining=1 → first byte=0xb0, second=0x01
		{objBlob, 16, []byte{0xb0, 0x01}},
		// commit (1), size 1: (1<<4)|1 = 0x11
		{objCommit, 1, []byte{0x11}},
	}
	for _, c := range cases {
		got := packTypeSize(c.objType, c.size)
		if !bytes.Equal(got, c.want) {
			t.Errorf("packTypeSize(%d,%d) = %x, want %x", c.objType, c.size, got, c.want)
		}
	}
}

func TestBuildSnapshot_packMagic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.yml"), []byte("name: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap := buildSnapshot(dir)
	if len(snap.packData) < 4 {
		t.Fatal("PACK too short")
	}
	if string(snap.packData[:4]) != "PACK" {
		t.Errorf("expected PACK magic, got %q", snap.packData[:4])
	}
}

func TestBuildSnapshot_deterministic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.yml"), []byte("name: a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s1 := buildSnapshot(dir)
	s2 := buildSnapshot(dir)
	if s1.commitSHA != s2.commitSHA {
		t.Error("commit SHA is not deterministic")
	}
	if !bytes.Equal(s1.packData, s2.packData) {
		t.Error("PACK data is not deterministic")
	}
}

func TestBuildSnapshot_emptyDir(t *testing.T) {
	snap := buildSnapshot(t.TempDir())
	if len(snap.packData) < 4 {
		t.Fatal("expected non-empty PACK even for empty dir")
	}
	if string(snap.packData[:4]) != "PACK" {
		t.Errorf("expected PACK magic, got %q", snap.packData[:4])
	}
}

func TestInfoRefs(t *testing.T) {
	dir := t.TempDir()
	req := httptest.NewRequest(http.MethodGet, "/portainer/git/info/refs?service=git-upload-pack", nil)
	w := httptest.NewRecorder()
	GitHandler(dir).ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: want 200 got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/x-git-upload-pack-advertisement" {
		t.Errorf("Content-Type: %q", ct)
	}
	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte("# service=git-upload-pack")) {
		t.Errorf("expected service line in body, got: %q", body)
	}
}

func TestUploadPack(t *testing.T) {
	dir := t.TempDir()
	snap := buildSnapshot(dir)
	want := string(pktLine("want " + string(rune(0x30)) + "\n")) // minimal want line
	_ = want // we don't care about exact parsing, just that we get NAK + PACK back

	req := httptest.NewRequest(http.MethodPost, "/portainer/git/git-upload-pack",
		bytes.NewBufferString("0000")) // flush only
	req.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	w := httptest.NewRecorder()
	GitHandler(dir).ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: want 200 got %d", resp.StatusCode)
	}
	body := w.Body.Bytes()
	// response must start with pkt-line "NAK\n" = "0008NAK\n"
	if !bytes.HasPrefix(body, []byte("0008NAK\n")) {
		t.Errorf("expected NAK response, got prefix %q", body[:min(16, len(body))])
	}
	// no side-band-64k in request body → raw PACK immediately after NAK
	after := body[8:]
	if len(after) < 4 || string(after[:4]) != "PACK" {
		t.Errorf("expected raw PACK after NAK, got %q", after[:min(16, len(after))])
	}
	_ = snap
}

func TestGitClone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "minio.yml"), []byte("name: minio\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "postgres.yml"), []byte("name: postgres\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(GitHandler(dir))
	defer srv.Close()

	cloneDir := t.TempDir()
	cmd := exec.Command("git", "clone", srv.URL, cloneDir)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git clone failed: %v\n%s", err, out)
	}

	for _, name := range []string{"minio.yml", "postgres.yml"} {
		data, err := os.ReadFile(filepath.Join(cloneDir, name))
		if err != nil {
			t.Errorf("file %q not in cloned repo: %v", name, err)
			continue
		}
		_ = data
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
