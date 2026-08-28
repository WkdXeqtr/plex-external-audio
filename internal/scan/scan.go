// Package scan locates external audio tracks that sit next to a video
// file instead of inside it.
//
// Media collections routinely keep alternative dubs as separate files:
//
//	Series/
//	  Episode 01.mkv
//	  Episode 01.mka                       - beside the video
//	  RUS Sound/[Group A]/Episode 01.mka   - in a nested folder
//	  RUS Sound/[Group B]/Episode 01.mka
//
// The obvious implementation rescans the folder of every single video,
// so a season of twelve episodes reads the same directory twelve times
// and a library of thousands of files spends its whole runtime in the
// file system. This package walks the tree exactly once, remembers the
// audio files of every directory subtree, and then answers Match for
// each video from memory.
package scan

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// audioExtensions lists the extensions accepted as an external audio
// track, without the leading dot and in lower case.
var audioExtensions = map[string]struct{}{
	"mka":    {},
	"mp3":    {},
	"m4a":    {},
	"aac":    {},
	"ac3":    {},
	"eac3":   {},
	"dts":    {},
	"flac":   {},
	"ogg":    {},
	"opus":   {},
	"wav":    {},
	"wma":    {},
	"mp2":    {},
	"mpa":    {},
	"thd":    {},
	"truehd": {},
}

// IsAudio reports whether name looks like an external audio track. Only
// the extension is inspected, and its case is irrelevant: the tools
// that produce these files write ".AC3" as readily as ".ac3".
func IsAudio(name string) bool {
	extension := filepath.Ext(name)
	if len(extension) < 2 {
		// Either there is no dot at all, or the name ends in one and
		// carries no extension to judge.
		return false
	}
	_, ok := audioExtensions[strings.ToLower(extension[1:])]
	return ok
}

const (
	// extendedLengthPrefix is the Windows extended-length path marker.
	// Both the Plex database and the transcoder command line hand us
	// paths carrying it, and it has to go before a path is compared or
	// used as a map key, otherwise one file shows up under two names.
	extendedLengthPrefix = `\\?\`

	// uncMarker is what follows the prefix on a network path, as in
	// `\\?\UNC\host\share`. Stripping the prefix there would leave
	// "UNC\host\share", which names nothing at all, so those paths are
	// handed back untouched.
	uncMarker = "UNC"
)

// CleanPath removes the Windows extended-length prefix from p, leaving
// every other path, including `\\?\UNC\...`, exactly as it was. It is
// applied to whatever enters BuildIndex and Match so that the two agree
// on how a directory is spelled.
func CleanPath(p string) string {
	if !strings.HasPrefix(p, extendedLengthPrefix) {
		return p
	}
	rest := p[len(extendedLengthPrefix):]
	// Windows accepts the marker in any case, so recognise it that way
	// rather than trusting the caller to shout it. A bare "UNC" with
	// nothing behind it is degenerate, but it is still a network path
	// and still nothing we can meaningfully shorten.
	if strings.EqualFold(rest, uncMarker) {
		return p
	}
	if len(rest) > len(uncMarker) &&
		strings.EqualFold(rest[:len(uncMarker)], uncMarker) &&
		rest[len(uncMarker)] == '\\' {
		return p
	}
	return rest
}

// caseInsensitiveNames records whether the file system compares names
// without regard to case. Windows is the platform this tool ships on,
// and there "Episode 01.MKA" and "episode 01.mka" name the same file.
// Everywhere else the comparison stays exact, so that two files which
// differ only in case are not silently merged into one.
var caseInsensitiveNames = runtime.GOOS == "windows"

// foldForCompare normalizes a path or a base name for comparison. For
// names that are already lower case the standard library hands back the
// original string, so an index of a plain library costs no extra
// allocations here.
func foldForCompare(s string) string {
	if !caseInsensitiveNames {
		return s
	}
	return strings.ToLower(s)
}

// audioFile is one indexed track.
type audioFile struct {
	// path is the file exactly as the walk reported it, and is what
	// Match hands back to the caller.
	path string

	// directory is the folded directory of the file. The string is
	// shared between all files of the same directory, so a folder with
	// a thousand tracks stores its name once.
	directory string

	// name is the base name, folded for comparison.
	name string
}

// Index holds every audio file below a scanned root, grouped so that a
// video can be answered without touching the file system again.
//
// Once BuildIndex has returned, an Index is read-only and therefore
// safe for concurrent use by multiple goroutines.
type Index struct {
	// files holds every audio file found, sorted by path. Match walks
	// the buckets in this order, which is what makes its output stable
	// from run to run.
	files []audioFile

	// subtrees maps a folded directory to the positions in files of the
	// audio files in that directory and in every directory below it. A
	// file is therefore listed under each of its ancestors up to the
	// scanned root, which is what lets Match pick up tracks that were
	// filed away in nested folders. Positions are stored rather than
	// copies because a file repeated at every level would otherwise
	// multiply the memory the index needs.
	subtrees map[string][]int32
}

// BuildIndex walks root once and records every audio file it finds.
//
// Directories that cannot be read are skipped rather than fatal: a
// media server is full of recycle bins, mount points and network shares
// that come and go, and one of them must not cost us the whole scan.
// Only a failure on root itself is reported, because then nothing was
// scanned at all.
//
// Symbolic links are neither followed nor indexed. Following them would
// risk walking in circles, and Plex stores the real path anyway.
func BuildIndex(root string) (*Index, error) {
	root = filepath.Clean(CleanPath(root))
	rootKey := foldForCompare(root)

	index := &Index{subtrees: make(map[string][]int32)}

	// directoryKeys interns the folded directory strings so that all
	// files of one folder point at a single copy of its name.
	directoryKeys := make(map[string]string)

	walk := func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			// Directories are descended into by returning nil; links,
			// devices and sockets are simply of no interest.
			return nil
		}
		if !IsAudio(entry.Name()) {
			return nil
		}
		key := foldForCompare(filepath.Dir(path))
		if shared, ok := directoryKeys[key]; ok {
			key = shared
		} else {
			directoryKeys[key] = key
		}
		index.files = append(index.files, audioFile{
			path:      path,
			directory: key,
			name:      foldForCompare(entry.Name()),
		})
		return nil
	}
	if err := filepath.WalkDir(root, walk); err != nil {
		return nil, fmt.Errorf("scan: walking %q: %w", root, err)
	}

	// Sorting once here is what buys Match a stable order for free: the
	// buckets below are filled in this order and read in that order.
	sort.Slice(index.files, func(a, b int) bool {
		return index.files[a].path < index.files[b].path
	})

	for position := range index.files {
		directory := index.files[position].directory
		for {
			index.subtrees[directory] = append(
				index.subtrees[directory], int32(position))
			if directory == rootKey {
				break
			}
			parent := filepath.Dir(directory)
			if parent == directory {
				// A volume or file system root was reached without
				// meeting the scanned root; there is nothing further
				// to attach this file to.
				break
			}
			directory = parent
		}
	}
	return index, nil
}

// Match returns the external audio tracks that belong to videoPath.
//
// A track belongs to the video when its name continues the video name,
// without the extension, at a dot: "Episode 01.mkv" owns both
// "Episode 01.mka" and "Episode 01.rus.Group.ac3", but neither
// "Episode 010.mka" nor "Some dub.mka". Tracks in the video's own
// folder and in every folder below it are considered; the video file
// itself never is.
//
// The result is ordered by full path, and that order is stable across
// runs, because the track numbers written into the Plex database must
// not shuffle when the tool runs again.
func (i *Index) Match(videoPath string) []string {
	if i == nil || len(i.files) == 0 {
		return nil
	}
	videoPath = filepath.Clean(CleanPath(videoPath))
	directory := foldForCompare(filepath.Dir(videoPath))
	bucket := i.subtrees[directory]
	if len(bucket) == 0 {
		return nil
	}

	base := filepath.Base(videoPath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if stem == "" {
		// There is nothing to anchor the comparison on, and matching
		// every track in the subtree would be far worse than matching
		// none of them.
		return nil
	}
	prefix := foldForCompare(stem) + "."
	self := foldForCompare(base)

	var matches []string
	for _, position := range bucket {
		file := &i.files[position]
		// The name has to be strictly longer than the prefix, since a
		// track needs at least an extension behind the dot.
		if len(file.name) <= len(prefix) ||
			file.name[:len(prefix)] != prefix {
			continue
		}
		if file.name == self && file.directory == directory {
			// The video may itself carry an audio extension, in which
			// case it matches its own prefix. Compare the directory as
			// well, so that a namesake in a nested folder survives.
			continue
		}
		matches = append(matches, file.path)
	}
	return matches
}

// Len reports how many audio files the index holds.
func (i *Index) Len() int {
	if i == nil {
		return 0
	}
	return len(i.files)
}
