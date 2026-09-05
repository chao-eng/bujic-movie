package mediautil

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

const nfoWithSubs = `<?xml version="1.0" encoding="utf-8" standalone="yes"?>
<movie>
  <title>Sample (2020)</title>
  <fileinfo>
    <streamdetails>
      <video><codec>hevc</codec></video>
      <audio><codec>eac3</codec><language>eng</language></audio>
      <subtitle><codec>subrip</codec><index>2</index><language>zho</language></subtitle>
      <subtitle><codec>subrip</codec><index>3</index><language>eng</language></subtitle>
    </streamdetails>
  </fileinfo>
</movie>
`

const nfoWithoutStreamDetails = `<?xml version="1.0" encoding="utf-8" standalone="yes"?>
<movie>
  <title>Old (2010)</title>
</movie>
`

func TestInternalSubsFromNFO(t *testing.T) {
	dir := t.TempDir()
	nfo := filepath.Join(dir, "movie.nfo")
	writeFile(t, nfo, nfoWithSubs)

	subs, trust := InternalSubsFromNFO(nfo)
	if !trust {
		t.Fatal("expected trust=true for nfo with streamdetails")
	}
	if len(subs) != 2 {
		t.Fatalf("expected 2 internal subs, got %d: %+v", len(subs), subs)
	}
	if subs[0].Language != "zh-CN" { // zho normalized
		t.Errorf("expected zho normalized to zh-CN, got %q", subs[0].Language)
	}
	if subs[0].Type != "internal" || subs[0].Index != 2 {
		t.Errorf("unexpected sub0: %+v", subs[0])
	}
	if subs[1].Language != "en" {
		t.Errorf("expected eng -> en, got %q", subs[1].Language)
	}
}

func TestInternalSubsFromNFOWithoutStreamDetails(t *testing.T) {
	dir := t.TempDir()
	nfo := filepath.Join(dir, "old.nfo")
	writeFile(t, nfo, nfoWithoutStreamDetails)

	_, trust := InternalSubsFromNFO(nfo)
	if trust {
		t.Error("expected trust=false when nfo lacks streamdetails")
	}
}

func TestInternalSubsFromNFOFileMissing(t *testing.T) {
	_, trust := InternalSubsFromNFO(filepath.Join(t.TempDir(), "nope.nfo"))
	if trust {
		t.Error("expected trust=false when nfo missing")
	}
}

func TestNFOForVideo(t *testing.T) {
	dir := t.TempDir()
	// movie.nfo present -> returned for any video in that dir
	writeFile(t, filepath.Join(dir, "movie.nfo"), nfoWithoutStreamDetails)
	got := NFOForVideo(filepath.Join(dir, "Any (2020) [1080p].mkv"))
	if filepath.Base(got) != "movie.nfo" {
		t.Errorf("expected movie.nfo, got %q", got)
	}

	// basename.nfo takes precedence when both exist
	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir2, "movie.nfo"), nfoWithoutStreamDetails)
	writeFile(t, filepath.Join(dir2, "Abc (2021).nfo"), nfoWithoutStreamDetails)
	got2 := NFOForVideo(filepath.Join(dir2, "Abc (2021).mkv"))
	if filepath.Base(got2) != "Abc (2021).nfo" {
		t.Errorf("expected basename.nfo precedence, got %q", got2)
	}

	// no nfo -> empty
	if got3 := NFOForVideo(filepath.Join(t.TempDir(), "Nothing.mkv")); got3 != "" {
		t.Errorf("expected empty, got %q", got3)
	}
}
