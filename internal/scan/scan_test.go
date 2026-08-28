package scan

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"
)

// tree is the sample layout every path test is built on. It mixes
// tracks beside the video, tracks buried in nested folders, near misses
// that must not be picked up, and a nested folder with a video of its
// own so that scoping can be checked.
var tree = []string{
	"Сериал/Серия 01.mkv",
	"Сериал/Серия 01.mka",
	"Сериал/Серия 01.rus.Название.ac3",
	"Сериал/Серия 01.srt",
	"Сериал/Серия 010.mkv",
	"Сериал/Серия 010.mka",
	"Сериал/Серия 02.mkv",
	"Сериал/Серия 02.MKA",
	"Сериал/Озвучка.mka",
	"Сериал/Постер.jpg",
	"Сериал/RUS Sound/[Группа A]/Серия 01.mka",
	"Сериал/RUS Sound/[Группа B]/Серия 01.mka",
	"Сериал/RUS Sound/[Группа B]/Серия 02.mka",
	"Сериал/Extras/Серия 02.mkv",
}

// buildTree writes the sample layout into a temporary directory and
// returns the directory together with a helper that turns a slash
// separated path from tree into a real one.
func buildTree(t *testing.T) (string, func(string) string) {
	t.Helper()
	root := t.TempDir()
	resolve := func(p string) string {
		return filepath.Join(root, filepath.FromSlash(p))
	}
	for _, name := range tree {
		full := resolve(name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("creating %q: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatalf("creating %q: %v", full, err)
		}
	}
	return root, resolve
}

// expect turns slash separated names from tree into the exact result
// Match must return: the same set, ordered by full path.
func expect(resolve func(string) string, names ...string) []string {
	if len(names) == 0 {
		return nil
	}
	wanted := make([]string, 0, len(names))
	for _, name := range names {
		wanted = append(wanted, resolve(name))
	}
	sort.Strings(wanted)
	return wanted
}

func indexTree(t *testing.T, root string) *Index {
	t.Helper()
	index, err := BuildIndex(root)
	if err != nil {
		t.Fatalf("BuildIndex(%q): %v", root, err)
	}
	return index
}

func TestIsAudio(t *testing.T) {
	cases := map[string]bool{
		"Серия 01.mka":              true,
		"Серия 01.MKA":              true,
		"Серия 01.rus.Название.ac3": true,
		"track.Eac3":                true,
		"track.truehd":              true,
		"track.thd":                 true,
		"track.opus":                true,
		"track.wav":                 true,
		"Серия 01.mkv":              false,
		"movie.mp4":                 false,
		"subtitles.srt":             false,
		"poster.jpg":                false,
		"track.mkast":               false,
		"README":                    false,
		"trailing.":                 false,
		"":                          false,
	}
	for name, wanted := range cases {
		if got := IsAudio(name); got != wanted {
			t.Errorf("IsAudio(%q) = %v, want %v", name, got, wanted)
		}
	}
}

func TestCleanPath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`\\?\E:\Media\Серия 01.mkv`, `E:\Media\Серия 01.mkv`},
		{`\\?\E:\`, `E:\`},
		// A UNC path keeps its prefix: without it the remainder would
		// name nothing at all.
		{`\\?\UNC\server\share\Серия 01.mkv`,
			`\\?\UNC\server\share\Серия 01.mkv`},
		{`\\?\unc\server\share\x.mka`, `\\?\unc\server\share\x.mka`},
		{`\\?\UNC`, `\\?\UNC`},
		// Anything without the marker is left exactly as it came in.
		{`E:\Media\Серия 01.mkv`, `E:\Media\Серия 01.mkv`},
		{`\\server\share\Серия 1.mkv`, `\\server\share\Серия 1.mkv`},
		{`/mnt/media/Серия 01.mkv`, `/mnt/media/Серия 01.mkv`},
		{`\\?\UNCLE\x.mka`, `UNCLE\x.mka`},
		{"", ""},
	}
	for _, c := range cases {
		if got := CleanPath(c.in); got != c.want {
			t.Errorf("CleanPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMatch(t *testing.T) {
	root, resolve := buildTree(t)
	index := indexTree(t, root)

	cases := []struct {
		name  string
		video string
		want  []string
	}{
		{
			// Everything named after the episode counts, beside the
			// video and in nested folders alike. "Серия 010.mka" is a
			// near miss and "Озвучка.mka" is unrelated.
			name:  "beside and nested",
			video: "Сериал/Серия 01.mkv",
			want: expect(resolve,
				"Сериал/Серия 01.mka",
				"Сериал/Серия 01.rus.Название.ac3",
				"Сериал/RUS Sound/[Группа A]/Серия 01.mka",
				"Сериал/RUS Sound/[Группа B]/Серия 01.mka",
			),
		},
		{
			// The longer number must not borrow the shorter one's
			// tracks, and must not lend it its own.
			name:  "longer episode number",
			video: "Сериал/Серия 010.mkv",
			want:  expect(resolve, "Сериал/Серия 010.mka"),
		},
		{
			// The extension of a track is matched without regard to
			// case on every platform.
			name:  "uppercase extension",
			video: "Сериал/Серия 02.mkv",
			want: expect(resolve,
				"Сериал/Серия 02.MKA",
				"Сериал/RUS Sound/[Группа B]/Серия 02.mka",
			),
		},
		{
			// A video in a nested folder sees only that folder and
			// below, never its parent, so the namesake track two
			// levels up stays where it is.
			name:  "nested video does not reach upwards",
			video: "Сериал/Extras/Серия 02.mkv",
			want:  nil,
		},
		{
			// A video whose own extension is an audio one matches its
			// own prefix and must not report itself. The namesakes in
			// the nested folders are a different file and do belong.
			name:  "video never matches itself",
			video: "Сериал/Серия 01.mka",
			want: expect(resolve,
				"Сериал/Серия 01.rus.Название.ac3",
				"Сериал/RUS Sound/[Группа A]/Серия 01.mka",
				"Сериал/RUS Sound/[Группа B]/Серия 01.mka",
			),
		},
		{
			name:  "unknown video",
			video: "Сериал/Серия 99.mkv",
			want:  nil,
		},
		{
			name:  "unknown directory",
			video: "Сериал/Нет папки/Серия 01.mkv",
			want:  nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := index.Match(resolve(c.video))
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Match(%q):\n got %q\nwant %q",
					c.video, got, c.want)
			}
			if !sort.StringsAreSorted(got) {
				t.Errorf("Match(%q) is not ordered by path: %q",
					c.video, got)
			}
		})
	}
}

// TestMatchIsStable guards the property the Plex track numbers depend
// on: the same tree must produce the same order every time, whether
// within one index or across a fresh one.
func TestMatchIsStable(t *testing.T) {
	root, resolve := buildTree(t)
	video := resolve("Сериал/Серия 01.mkv")

	first := indexTree(t, root).Match(video)
	if len(first) == 0 {
		t.Fatal("expected the sample tree to produce matches")
	}
	for round := 0; round < 3; round++ {
		again := indexTree(t, root).Match(video)
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("round %d differs:\nfirst %q\nagain %q",
				round, first, again)
		}
	}
	repeated := indexTree(t, root)
	if !reflect.DeepEqual(repeated.Match(video), repeated.Match(video)) {
		t.Error("two calls on one index disagree")
	}
}

func TestLen(t *testing.T) {
	root, _ := buildTree(t)

	wanted := 0
	for _, name := range tree {
		if IsAudio(name) {
			wanted++
		}
	}
	if got := indexTree(t, root).Len(); got != wanted {
		t.Errorf("Len() = %d, want %d", got, wanted)
	}
	if got := (*Index)(nil).Len(); got != 0 {
		t.Errorf("Len() on a nil index = %d, want 0", got)
	}
	if got := (*Index)(nil).Match("anything.mkv"); got != nil {
		t.Errorf("Match on a nil index = %q, want nil", got)
	}
	var empty Index
	if got := empty.Match("anything.mkv"); got != nil {
		t.Errorf("Match on an empty index = %q, want nil", got)
	}
}

// TestBuildIndexEmptyAndMissingRoot covers the two ends of the range: a
// root that holds nothing is fine, a root that is not there is not.
func TestBuildIndexEmptyAndMissingRoot(t *testing.T) {
	empty := t.TempDir()
	index := indexTree(t, empty)
	if index.Len() != 0 {
		t.Errorf("Len() = %d on an empty root, want 0", index.Len())
	}

	missing := filepath.Join(empty, "нет такого каталога")
	if _, err := BuildIndex(missing); err == nil {
		t.Errorf("BuildIndex(%q) succeeded, want an error", missing)
	}
}

// TestExtendedLengthPaths checks rule five end to end: a `\\?\` path may
// be handed to either entry point, in any combination, and still finds
// the same tracks.
func TestExtendedLengthPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the extended-length prefix is a Windows spelling")
	}
	root, resolve := buildTree(t)
	video := resolve("Сериал/Серия 01.mkv")
	want := expect(resolve,
		"Сериал/Серия 01.mka",
		"Сериал/Серия 01.rus.Название.ac3",
		"Сериал/RUS Sound/[Группа A]/Серия 01.mka",
		"Сериал/RUS Sound/[Группа B]/Серия 01.mka",
	)

	plain := indexTree(t, root)
	extended := indexTree(t, `\\?\`+root)
	cases := []struct {
		name  string
		index *Index
		video string
	}{
		{"extended root", extended, video},
		{"extended video", plain, `\\?\` + video},
		{"both extended", extended, `\\?\` + video},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.index.Match(c.video); !reflect.DeepEqual(got, want) {
				t.Errorf("Match(%q):\n got %q\nwant %q",
					c.video, got, want)
			}
		})
	}
}

// TestMatchIgnoresCaseOnWindows covers rule six. Elsewhere names are
// compared exactly, because two files differing only in case are two
// different files there.
func TestMatchIgnoresCaseOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("names are compared case sensitively on this platform")
	}
	root, resolve := buildTree(t)
	index := indexTree(t, root)
	want := expect(resolve,
		"Сериал/Серия 01.mka",
		"Сериал/Серия 01.rus.Название.ac3",
		"Сериал/RUS Sound/[Группа A]/Серия 01.mka",
		"Сериал/RUS Sound/[Группа B]/Серия 01.mka",
	)

	shouted := filepath.Join(root, "СЕРИАЛ", "СЕРИЯ 01.MKV")
	if got := index.Match(shouted); !reflect.DeepEqual(got, want) {
		t.Errorf("Match(%q):\n got %q\nwant %q", shouted, got, want)
	}
}
