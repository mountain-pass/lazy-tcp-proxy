# Portainer Git HTTP Endpoint — Implementation Plan

**Requirement**: [2026-06-04-portainer-git-http-endpoint.md](2026-06-04-portainer-git-http-endpoint.md)
**Date**: 2026-06-04
**Status**: Implemented

## Implementation Steps

1. **Create `lazy-tcp-proxy/internal/portainer/git.go`** — git Smart HTTP server
2. **Modify `lazy-tcp-proxy/internal/portainer/templates.go`** — replace inline `compose` with `repository` object; accept `baseURL` parameter
3. **Modify `lazy-tcp-proxy/main.go`** — rename `/portainer` → `/portainer/templates`; register `/portainer/git/` handler; update log line

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/internal/portainer/git.go` | Create | Git Smart HTTP: object hashing, PACK generation, info/refs + git-upload-pack handlers |
| `lazy-tcp-proxy/internal/portainer/git_test.go` | Create | Unit tests: object hashing, PACK round-trip, clone via `git clone` |
| `lazy-tcp-proxy/internal/portainer/templates.go` | Modify | Replace `Compose`/`Logo`/`Description` fields with `Repository`; `BuildTemplates` takes `baseURL string` |
| `lazy-tcp-proxy/internal/portainer/templates_test.go` | Modify | Update tests for new `Repository` field shape |
| `lazy-tcp-proxy/main.go` | Modify | Rename route, register git handler, update log |

## API Contracts

### `GET /portainer/templates`
Unchanged response shape except template object:
```json
{
  "version": "2",
  "templates": [
    {
      "type": 3,
      "title": "minio",
      "repository": {
        "url": "http://<host>/portainer/git",
        "stackfile": "minio.yml"
      },
      "env": [ ... ]
    }
  ]
}
```

### `GET /portainer/git/info/refs?service=git-upload-pack`
```
Content-Type: application/x-git-upload-pack-advertisement

001e# service=git-upload-pack\n
0000
<pkt-line: "<sha> HEAD\0side-band-64k symref=HEAD:refs/heads/main agent=lazy-tcp-proxy\n">
<pkt-line: "<sha> refs/heads/main\n">
0000
```

### `POST /portainer/git/git-upload-pack`
Request body (pkt-line):
```
0032want <sha>\n
00000009done\n
```
Response:
```
Content-Type: application/x-git-upload-pack-result

0008NAK\n
<raw PACK bytes>
```

## Data Models

### Updated Template struct
```go
type Repository struct {
    URL       string `json:"url"`
    StackFile string `json:"stackfile"`
}

type Template struct {
    Type       int        `json:"type"`
    Title      string     `json:"title"`
    Repository Repository `json:"repository"`
    Env        []EnvVar   `json:"env"`
}
```

### Git object types (PACK)
```
OBJ_COMMIT = 1
OBJ_TREE   = 2
OBJ_BLOB   = 3
```

### repoSnapshot — in-memory git state computed from RECIPES_DIR
```go
type repoSnapshot struct {
    blobSHAs  map[string][20]byte  // filename → blob SHA
    treeSHA   [20]byte
    commitSHA [20]byte
    packData  []byte
}
```

## Key Code Snippets

### Git object hashing
```go
func gitObject(objType, content []byte) (sha [20]byte, compressed []byte) {
    header := fmt.Sprintf("%s %d\x00", objType, len(content))
    data := append([]byte(header), content...)
    sha = sha1.Sum(data)
    var buf bytes.Buffer
    w := zlib.NewWriter(&buf)
    w.Write(data)
    w.Close()
    return sha, buf.Bytes()
}
```

### Tree object entry format (binary)
```
"100644 <filename>\x00" + <20-byte blob SHA>
```
Files sorted lexicographically. Tree hash computed over the full concatenation.

### Commit object
```
tree <tree-sha-hex>\n
author lazy-tcp-proxy <noreply@localhost> 0 +0000\n
committer lazy-tcp-proxy <noreply@localhost> 0 +0000\n
\n
recipes\n
```

### PACK file structure
```
[4]  "PACK"
[4]  version = 2  (big-endian)
[4]  object count (big-endian)
...  per-object: type/size varint + zlib-compressed object data
[20] SHA1 of everything above
```

Per-object type/size varint: first byte = `(type << 4) | (size & 0xf)`, MSB set if more bytes follow; subsequent bytes = 7 bits of size, MSB set if more follow.

### pkt-line helpers
```go
func pktLine(s string) []byte {
    n := len(s) + 4
    return []byte(fmt.Sprintf("%04x%s", n, s))
}
var pktFlush = []byte("0000")
```

### `buildSnapshot(recipesDir string) repoSnapshot`
1. Glob `*.yml` from `recipesDir`, sort filenames
2. For each file: read content, compute blob SHA + compressed bytes
3. Build tree object bytes (binary entries sorted by name), compute tree SHA + compressed bytes
4. Build commit object bytes, compute commit SHA + compressed bytes
5. Assemble PACK: header + 3 object entries (blobs + tree + commit) + trailing SHA1
6. Return `repoSnapshot` with all SHAs and PACK data

### `GitHandler(recipesDir string) http.Handler`
Routes on `r.URL.Path`:
- `…/info/refs` → `serveInfoRefs`
- `…/git-upload-pack` → `serveUploadPack`
- else → 404

### `serveInfoRefs(snap repoSnapshot, w, r)`
```
write: pktLine("# service=git-upload-pack\n") + pktFlush
write: pktLine(fmt.Sprintf("%x HEAD\x00side-band-64k symref=HEAD:refs/heads/main agent=lazy-tcp-proxy\n", snap.commitSHA))
write: pktLine(fmt.Sprintf("%x refs/heads/main\n", snap.commitSHA))
write: pktFlush
```

### `serveUploadPack(snap repoSnapshot, w, r)`
```
read and discard pkt-line "want" lines from body
write: pktLine("NAK\n")
write: snap.packData
```

### `main.go` wiring
```go
mux.HandleFunc("/portainer/templates", portainerpkg.TemplatesHandler(recipesDir))
mux.HandleFunc("/portainer/git/", portainerpkg.GitHandler(recipesDir).ServeHTTP)
```

`TemplatesHandler` extracts base URL from `r.Host`:
```go
scheme := "http"
if r.TLS != nil { scheme = "https" }
baseURL := scheme + "://" + r.Host
```

## Unit Tests

| Test | Input | Expected Output |
|------|-------|-----------------|
| `TestBlobSHA` | known content | SHA matches `git hash-object` output |
| `TestTreeObject` | two files | tree SHA matches `git mktree` output |
| `TestBuildSnapshot_empty` | empty dir | valid PACK with 1 commit, 1 empty tree, 0 blobs |
| `TestBuildSnapshot_singleFile` | one `.yml` file | PACK contains blob + tree + commit; commitSHA deterministic |
| `TestInfoRefs` | mock request | valid pkt-line response, correct SHA |
| `TestUploadPack` | mock POST with want line | response starts with `0008NAK\n`, followed by PACK magic |
| `TestGitClone` | integration: `exec.Command("git","clone",…)` against test server | cloned repo contains expected files |
| `TestTemplates_repositoryField` | single recipe file | template has `repository.url` and `repository.stackfile`, no `compose` field |

## Risks & Open Questions

- **`git clone` integration test**: requires `git` binary present in the test environment. The test should be skipped (`t.Skip`) if `git` is not in `$PATH`.
- **Empty RECIPES_DIR**: an empty tree + commit is valid git; `git clone` succeeds with an empty working tree. This is correct behaviour.
- **PACK trailing checksum**: the SHA1 must cover all bytes written to the PACK, including the header. Compute with a `sha1.New()` fed via `io.MultiWriter` during PACK assembly.
- **Side-band multiplexing**: Portainer's git client may expect side-band-64k framing for the PACK data. If bare PACK doesn't work, wrap each chunk as `\x01<data>` pkt-lines. Start without side-band and add if needed.
