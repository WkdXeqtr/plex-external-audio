// Command mapper teaches Plex about audio files that live next to the video.
//
// It walks the library, finds sidecar audio files, asks ffprobe what is in
// them, and writes a row into Plex's media_streams for each track it finds.
// Plex then lists those tracks as if they had been inside the video all along,
// and asks its transcoder for them by index - which is where the wrapper takes
// over.
//
// Two things make this fast enough to run over a whole library. Each directory
// is walked exactly once, no matter how many episodes sit in it, and every
// audio file is probed concurrently rather than one process at a time.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/WkdXeqtr/plex-external-audio/internal/plex"
	"github.com/WkdXeqtr/plex-external-audio/internal/probe"
	"github.com/WkdXeqtr/plex-external-audio/internal/scan"
)

func main() {
	var (
		// -dbPath rather than -db: the guard and the tray already call the
		// mapper by this name, and they are configured by an installer that may
		// be older than the binary it is pointing at.
		dbPath       = flag.String("dbPath", "", "path to com.plexapp.plugins.library.db (found automatically when empty)")
		ffprobePath  = flag.String("ffprobe", "ffprobe", "path to the ffprobe executable")
		clean        = flag.Bool("clean", false, "remove everything this tool added and exit")
		dryRun       = flag.Bool("n", false, "report what would change and write nothing")
		folderTitles = flag.Bool("folder-titles", true, "name an untitled track after the folder holding it")
		keepMissing  = flag.Bool("keep-missing", false, "do not remove rows whose audio file has disappeared")
		workers      = flag.Int("workers", 0, "how many ffprobe processes to run at once (0 = one per CPU)")
	)
	flag.Parse()

	path := *dbPath
	if path == "" {
		found, err := plex.DefaultDBPath()
		if err != nil {
			fail(err)
		}
		path = found
	}
	db, err := plex.Open(path)
	if err != nil {
		fail(err)
	}
	defer db.Close()
	fmt.Println("Database:", path)

	if *clean {
		runClean(db, *dryRun)
		return
	}

	prober := probe.Prober{Exe: *ffprobePath}
	if err := prober.Check(); err != nil {
		fail(fmt.Errorf("ffprobe is not usable: %w", err))
	}

	if err := run(db, prober, options{
		dryRun:       *dryRun,
		folderTitles: *folderTitles,
		keepMissing:  *keepMissing,
		workers:      *workers,
	}); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

type options struct {
	dryRun       bool
	folderTitles bool
	keepMissing  bool
	workers      int
}

// runClean undoes everything this tool has ever written.
func runClean(db *plex.DB, dryRun bool) {
	existing, err := db.Externals()
	if err != nil {
		fail(err)
	}
	if dryRun {
		fmt.Printf("Would remove %d external audio rows.\n", len(existing))
		return
	}
	n, err := db.DeleteAll()
	if err != nil {
		fail(err)
	}
	// The rows are gone; anything still pointing at them would take the player
	// down the next time its audio menu is opened.
	repaired, err := db.RepairSelections()
	if err != nil {
		fail(err)
	}
	fmt.Printf("Removed %d external audio rows.\n", n)
	if repaired > 0 {
		fmt.Printf("Cleared %d stale track selection(s).\n", repaired)
	}
}

func run(db *plex.DB, prober probe.Prober, opt options) error {
	videos, err := db.Videos()
	if err != nil {
		return err
	}
	existing, err := db.Externals()
	if err != nil {
		return err
	}
	fmt.Printf("Library: %d video files, %d external tracks already known.\n",
		len(videos), len(existing))

	// What is already in the database, keyed the way we will look it up.
	known := make(map[string]bool, len(existing))       // audio path + stream
	nextIndex := make(map[int64]int, len(videos))       // per part
	for _, e := range existing {
		known[streamKey(e.Path, e.SubIndex)] = true
		if e.Index >= nextIndex[e.PartID] {
			nextIndex[e.PartID] = e.Index + 1
		}
	}

	if !opt.keepMissing {
		if err := dropVanished(db, existing, opt.dryRun); err != nil {
			return err
		}
	}

	// --- find the candidates -------------------------------------------------
	//
	// One index per directory, reused by every video in it. Walking the
	// directory again for each episode is what made the original slow: a season
	// folder with twelve episodes got walked twelve times.
	indexes := map[string]*scan.Index{}
	type candidate struct {
		video Video
		audio string
	}
	var candidates []candidate
	audioSet := map[string]bool{}

	for _, v := range videos {
		if !v.Analysed {
			fmt.Printf("  ! not analysed by Plex yet, skipping: %s\n", v.Path)
			continue
		}
		dir := filepath.Dir(v.Path)
		idx, ok := indexes[dir]
		if !ok {
			idx, err = scan.BuildIndex(dir)
			if err != nil {
				fmt.Printf("  ! cannot read %s: %v\n", dir, err)
				indexes[dir] = nil
				continue
			}
			indexes[dir] = idx
		}
		if idx == nil {
			continue
		}
		for _, a := range idx.Match(v.Path) {
			candidates = append(candidates, candidate{video: Video(v), audio: a})
			audioSet[a] = true
		}
	}

	// Probe only the files we might actually add. A file already fully present
	// in the database still has to be probed if it holds several streams and
	// only some are known, so the check is per stream, later.
	paths := make([]string, 0, len(audioSet))
	for p := range audioSet {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	fmt.Printf("Found %d audio files to inspect.\n", len(paths))

	results := prober.Many(context.Background(), paths, opt.workers)

	// --- write ---------------------------------------------------------------
	added := 0
	for _, c := range candidates {
		res, ok := results[c.audio]
		if !ok {
			continue
		}
		if res.Err != nil {
			fmt.Printf("  ! ffprobe failed on %s: %v\n", c.audio, res.Err)
			continue
		}
		for _, st := range res.Streams {
			if st.CodecType != "audio" {
				continue
			}
			if known[streamKey(c.audio, st.Index)] {
				continue
			}
			idx := nextIndex[c.video.PartID]
			if idx < plex.ExternalIndexBase {
				idx = plex.ExternalIndexBase
			}
			if c.video.MaxIndex >= idx {
				idx = c.video.MaxIndex + 1
			}

			s := build(c.video, c.audio, st, idx, opt.folderTitles)
			if opt.dryRun {
				fmt.Printf("  ~ would add %s [%s %s] as stream %d of %s\n",
					title(s), s.Codec, s.Language, s.Index, filepath.Base(c.video.Path))
			} else if err := db.Insert(s); err != nil {
				fmt.Printf("  ! %v\n", err)
				continue
			} else {
				fmt.Printf("  + %s [%s %s] -> stream %d of %s\n",
					title(s), s.Codec, s.Language, s.Index, filepath.Base(c.video.Path))
			}
			known[streamKey(c.audio, st.Index)] = true
			nextIndex[c.video.PartID] = idx + 1
			added++
		}
	}

	if !opt.dryRun {
		// Always, whatever this run did: rows may also have vanished between
		// runs, when Plex re-analysed a file and wiped what we had added.
		if repaired, err := db.RepairSelections(); err != nil {
			return err
		} else if repaired > 0 {
			fmt.Printf("Cleared %d stale track selection(s).\n", repaired)
		}
	}

	// The line starts with "Done." because the guard picks the summary out of
	// this output by that prefix and logs it; everything else it swallows.
	verb := "added"
	if opt.dryRun {
		verb = "would add"
	}
	fmt.Printf("Done. Audio tracks %s: %d\n", verb, added)
	return nil
}

// Video mirrors plex.Video so the rest of this file reads without the package
// prefix on every line.
type Video = plex.Video

func streamKey(path string, sub int) string {
	return strings.ToLower(path) + "#" + strconv.Itoa(sub)
}

func title(s plex.NewStream) string {
	if t := s.Extra["ma:title"]; t != "" {
		return t
	}
	return filepath.Base(s.Path)
}

// dropVanished removes rows whose audio file is no longer on disk.
func dropVanished(db *plex.DB, existing []plex.External, dryRun bool) error {
	var gone []int64
	for _, e := range existing {
		if _, err := os.Stat(e.Path); err != nil {
			gone = append(gone, e.ID)
			fmt.Printf("  - audio file is gone: %s\n", e.Path)
		}
	}
	if len(gone) == 0 {
		return nil
	}
	if dryRun {
		fmt.Printf("Would remove %d rows for missing files.\n", len(gone))
		return nil
	}
	n, err := db.Delete(gone)
	if err != nil {
		return err
	}
	fmt.Printf("Removed %d rows for missing files.\n", n)
	return nil
}

// build turns one probed stream into a row ready for the database.
func build(v Video, audioPath string, st probe.Stream, index int, folderTitles bool) plex.NewStream {
	extra := map[string]string{}
	if st.ChannelLayout != "" {
		extra["ma:audioChannelLayout"] = st.ChannelLayout
	}
	if st.SampleRate != "" {
		extra["ma:samplingRate"] = st.SampleRate
	}
	if p := profile(st.Profile); p != "" {
		extra["ma:profile"] = p
	}

	lang, fileTitle := fromFilename(v.Path, audioPath)
	if t := st.Tags["title"]; t != "" {
		fileTitle = t
	} else if fileTitle == "" && folderTitles {
		fileTitle = folderName(v.Path, audioPath)
	}
	if fileTitle != "" {
		extra["ma:title"] = fileTitle
	}

	language := plex.Language(st.Tags["language"])
	if language == "" {
		language = plex.Language(lang)
	}

	bitrate, _ := strconv.Atoi(st.BitRate)

	return plex.NewStream{
		ItemID:   v.ItemID,
		PartID:   v.PartID,
		Index:    index,
		Path:     audioPath,
		SubIndex: st.Index,
		Codec:    st.CodecName,
		Language: language,
		Channels: st.Channels,
		Bitrate:  bitrate,
		Extra:    extra,
	}
}

// profile maps ffprobe's profile string onto what Plex records.
//
// Only the DTS family carries a profile Plex cares about; everything else is
// left out rather than passed through, because an unrecognised value there is
// worse than none.
func profile(p string) string {
	switch p {
	case "DTS":
		return "dts"
	case "DTS-HD MA":
		return "ma"
	case "DTS-ES", "DTS-HD HRA":
		return strings.ToLower(strings.ReplaceAll(p, " ", "-"))
	}
	return ""
}

// fromFilename reads what the filename itself says about the track.
//
// The convention is VIDEO.lang.Title.ext, both parts optional: given
// "Episode 01.mkv" the file "Episode 01.rus.Studio Name.mka" means Russian,
// titled "Studio Name". A three-letter first component is taken as a language
// code; anything else is treated as the beginning of the title.
func fromFilename(videoPath, audioPath string) (lang, title string) {
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	name := filepath.Base(audioPath)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	if !strings.HasPrefix(strings.ToLower(name), strings.ToLower(base)+".") {
		return "", ""
	}
	rest := name[len(base)+1:]
	if rest == "" {
		return "", ""
	}
	parts := strings.Split(rest, ".")
	if len(parts[0]) == 3 && plex.Language(parts[0]) != "" {
		return parts[0], strings.Join(parts[1:], ".")
	}
	return "", rest
}

// folderName is the last resort for a track title: the folder holding the audio
// file. In a typical release that is the name of the group that made the dub,
// which is the only thing telling two of them apart.
func folderName(videoPath, audioPath string) string {
	dir := filepath.Dir(audioPath)
	if dir == filepath.Dir(videoPath) {
		return "" // sitting next to the video says nothing about it
	}
	return strings.Trim(filepath.Base(dir), "[]() ")
}
