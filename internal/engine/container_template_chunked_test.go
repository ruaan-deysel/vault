package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ruaan-deysel/vault/internal/dedup"
)

// withTestTemplatesDir points the Unraid template directory at a temporary
// one for the duration of a test.
func withTestTemplatesDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := dockerTemplatesDir
	dockerTemplatesDir = dir
	t.Cleanup(func() { dockerTemplatesDir = orig })
	return dir
}

func TestContainerTemplatePath(t *testing.T) {
	dir := withTestTemplatesDir(t)
	if got, want := containerTemplatePath("plex"), filepath.Join(dir, "my-plex.xml"); got != want {
		t.Errorf("containerTemplatePath() = %q, want %q", got, want)
	}
}

// TestWriteChunkedRestoreSidecars is the restore half of issue #379. The
// chunked format keeps the template in the manifest rather than as a file, so
// it has to be materialised into a directory shaped like a classic backup's —
// otherwise the shared restore path finds nothing and the Docker page's Edit
// form keeps pointing at whatever template happened to still be on the flash
// drive.
func TestWriteChunkedRestoreSidecars(t *testing.T) {
	const templateXML = `<Container><Name>plex</Name></Container>`
	const dumpSQL = "-- logical dump\nSELECT 1;\n"

	cases := []struct {
		name         string
		template     string
		dump         string
		replay       bool
		wantDir      bool
		wantTemplate string
		wantDump     string
		wantReplay   bool
	}{
		{
			name: "no sidecars materialises nothing",
		},
		{
			name:         "template only",
			template:     templateXML,
			wantDir:      true,
			wantTemplate: templateXML,
		},
		{
			name:     "dump only",
			dump:     dumpSQL,
			wantDir:  true,
			wantDump: dumpSQL,
		},
		{
			name:         "both share one directory",
			template:     templateXML,
			dump:         dumpSQL,
			replay:       true,
			wantDir:      true,
			wantTemplate: templateXML,
			wantDump:     dumpSQL,
			wantReplay:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, _, cleanup := dedup.NewTestRepoForEngine(t)
			defer cleanup()

			m := dedup.Manifest{Files: map[string]dedup.ManifestEntry{}}
			staging := t.TempDir()
			if tc.template != "" {
				m.Files[containerTemplateKey] = chunkTestFile(t, repo, filepath.Join(staging, "template"), tc.template)
			}
			if tc.dump != "" {
				m.Files[ContainerDBDumpKey] = chunkTestFile(t, repo, filepath.Join(staging, "dump"), tc.dump)
			}
			if tc.replay {
				m.Files[ContainerDBReplayKey] = dedup.ManifestEntry{}
			}
			if err := repo.Flush(); err != nil {
				t.Fatal(err)
			}

			dir, cleanupDir, err := writeChunkedRestoreSidecars(repo, m)
			if err != nil {
				t.Fatalf("writeChunkedRestoreSidecars: %v", err)
			}
			if cleanupDir != nil {
				defer cleanupDir()
			}
			if !tc.wantDir {
				if dir != "" {
					t.Fatalf("expected no directory, got %q", dir)
				}
				return
			}
			if dir == "" {
				t.Fatal("expected a materialised directory")
			}

			assertFile := func(name, want string) {
				t.Helper()
				data, readErr := os.ReadFile(filepath.Join(dir, name))
				if want == "" {
					if readErr == nil {
						t.Errorf("%s should not have been written", name)
					}
					return
				}
				if readErr != nil {
					t.Errorf("reading %s: %v", name, readErr)
					return
				}
				if string(data) != want {
					t.Errorf("%s = %q, want %q", name, data, want)
				}
			}
			assertFile(containerTemplateFile, tc.wantTemplate)
			assertFile(DatabaseDumpFile, tc.wantDump)

			// The replay marker decides whether the dump is pushed back into
			// the live server, so it has to survive the materialisation.
			_, markerErr := os.Stat(filepath.Join(dir, DatabaseReplayMarker))
			if tc.wantReplay && markerErr != nil {
				t.Errorf("replay marker missing: %v", markerErr)
			}
			if !tc.wantReplay && markerErr == nil {
				t.Error("replay marker should not have been written")
			}
		})
	}
}

// chunkTestFile writes content to path and chunks it into repo, returning the
// manifest entry a backup would have recorded.
func chunkTestFile(t *testing.T, repo *dedup.Repo, path, content string) dedup.ManifestEntry {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	entry, err := chunkFileIntoRepo(repo, path)
	if err != nil {
		t.Fatalf("chunkFileIntoRepo: %v", err)
	}
	return entry
}
