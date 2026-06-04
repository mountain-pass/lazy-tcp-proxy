package portainer

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	objCommit = 1
	objTree   = 2
	objBlob   = 3
)

type repoSnapshot struct {
	commitSHA [20]byte
	packData  []byte
}

// gitObjectData returns the full loose-object byte string used for SHA1 hashing:
// "<type> <size>\x00<content>"
func gitObjectData(objType string, content []byte) []byte {
	header := fmt.Sprintf("%s %d\x00", objType, len(content))
	out := make([]byte, len(header)+len(content))
	copy(out, header)
	copy(out[len(header):], content)
	return out
}

func gitSHA(objType string, content []byte) [20]byte {
	return sha1.Sum(gitObjectData(objType, content))
}

func zlibCompress(data []byte) []byte {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, _ = w.Write(data)
	_ = w.Close()
	return buf.Bytes()
}

// packTypeSize encodes the PACK per-object type+size varint header.
// First byte: bits 6-4 = type, bits 3-0 = low 4 bits of size, bit 7 = more.
// Subsequent bytes: bits 6-0 = next 7 bits of size, bit 7 = more.
func packTypeSize(objType, size int) []byte {
	var buf []byte
	b := byte((objType << 4) | (size & 0xf))
	size >>= 4
	for size > 0 {
		buf = append(buf, b|0x80)
		b = byte(size & 0x7f)
		size >>= 7
	}
	return append(buf, b)
}

// buildSnapshot reads all *.yml files from recipesDir and builds an in-memory
// git repository snapshot: blob objects, a flat tree, one commit, and a PACK
// file containing all objects. The commit SHA is deterministic for identical
// file contents.
func buildSnapshot(recipesDir string) repoSnapshot {
	entries, _ := filepath.Glob(filepath.Join(recipesDir, "*.yml"))
	sort.Strings(entries)

	type blobEntry struct {
		name    string
		sha     [20]byte
		content []byte
	}

	blobs := make([]blobEntry, 0, len(entries))
	for _, path := range entries {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		blobs = append(blobs, blobEntry{
			name:    filepath.Base(path),
			sha:     gitSHA("blob", data),
			content: data,
		})
	}

	// Tree object: binary entries sorted lexicographically by filename.
	// Each entry: "100644 <name>\x00" + 20-byte raw blob SHA.
	var treeBuf bytes.Buffer
	for _, b := range blobs {
		_, _ = fmt.Fprintf(&treeBuf, "100644 %s\x00", b.name)
		_, _ = treeBuf.Write(b.sha[:])
	}
	treeBytes := treeBuf.Bytes()
	treeSHA := gitSHA("tree", treeBytes)

	// Commit object.
	commitBytes := []byte(fmt.Sprintf(
		"tree %x\nauthor lazy-tcp-proxy <noreply@localhost> 0 +0000\ncommitter lazy-tcp-proxy <noreply@localhost> 0 +0000\n\nrecipes\n",
		treeSHA,
	))
	commitSHA := gitSHA("commit", commitBytes)

	// Assemble PACK file. SHA1 is computed over all bytes except the trailing
	// checksum itself via an io.MultiWriter tee into a running hash.
	var pack bytes.Buffer
	h := sha1.New()
	w := io.MultiWriter(&pack, h)

	_, _ = w.Write([]byte("PACK"))
	_ = binary.Write(w, binary.BigEndian, uint32(2))
	_ = binary.Write(w, binary.BigEndian, uint32(len(blobs)+2)) // blobs + tree + commit

	for _, b := range blobs {
		_, _ = w.Write(packTypeSize(objBlob, len(b.content)))
		_, _ = w.Write(zlibCompress(b.content))
	}
	_, _ = w.Write(packTypeSize(objTree, len(treeBytes)))
	_, _ = w.Write(zlibCompress(treeBytes))
	_, _ = w.Write(packTypeSize(objCommit, len(commitBytes)))
	_, _ = w.Write(zlibCompress(commitBytes))

	_, _ = pack.Write(h.Sum(nil)) // trailing SHA1 not included in checksum

	return repoSnapshot{commitSHA: commitSHA, packData: pack.Bytes()}
}

func pktLine(s string) []byte {
	n := len(s) + 4
	return []byte(fmt.Sprintf("%04x%s", n, s))
}

var pktFlush = []byte("0000")

func serveInfoRefs(snap repoSnapshot, w http.ResponseWriter) {
	sha := fmt.Sprintf("%x", snap.commitSHA)
	w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(pktLine("# service=git-upload-pack\n"))
	_, _ = w.Write(pktFlush)
	_, _ = w.Write(pktLine(sha + " HEAD\x00symref=HEAD:refs/heads/main agent=lazy-tcp-proxy\n"))
	_, _ = w.Write(pktLine(sha + " refs/heads/main\n"))
	_, _ = w.Write(pktFlush)
}

func serveUploadPack(snap repoSnapshot, w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
	w.Header().Set("Cache-Control", "no-cache")

	// Check if the client advertised sideband-64k or sideband capability.
	useSideband := bytes.Contains(body, []byte("sideband"))

	_, _ = w.Write(pktLine("NAK\n"))

	if useSideband {
		// Wrap PACK data in sideband pkt-lines: prefix each chunk with \x01 (data channel).
		const chunkSize = 65516 // max pkt-line payload minus 1 byte for sideband prefix
		for len(snap.packData) > 0 {
			n := len(snap.packData)
			if n > chunkSize {
				n = chunkSize
			}
			chunk := append([]byte{0x01}, snap.packData[:n]...)
			_, _ = w.Write(pktLine(string(chunk)))
			snap.packData = snap.packData[n:]
		}
		_, _ = w.Write(pktFlush)
	} else {
		_, _ = w.Write(snap.packData)
	}
}

// GitHandler returns an http.Handler that serves a read-only git Smart HTTP
// endpoint. Portainer (and git clients) can clone it to access all *.yml
// files in recipesDir. The repository is regenerated in-memory on every
// request — no persistent state is stored on disk.
func GitHandler(recipesDir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snap := buildSnapshot(recipesDir)
		switch {
		case strings.HasSuffix(r.URL.Path, "/info/refs"):
			serveInfoRefs(snap, w)
		case strings.HasSuffix(r.URL.Path, "/git-upload-pack"):
			serveUploadPack(snap, w, r)
		default:
			http.NotFound(w, r)
		}
	})
}
