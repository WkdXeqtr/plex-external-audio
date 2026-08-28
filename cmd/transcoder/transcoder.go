// Command transcoder stands in for Plex Transcoder and feeds it external audio.
//
// Plex has no notion of an audio track living in a separate file. The mapper
// makes it believe otherwise by adding rows to media_streams with stream
// indices at or above 1000, and Plex then asks its transcoder for stream 1001
// of a file that has no such stream. This program is what actually answers.
//
// It is installed in place of Plex Transcoder.exe, with the real binary parked
// beside it as Plex Transcoder_org.exe. On every launch it reads the command
// line Plex built, and if that command line mentions one of our stream indices
// it opens the external file as a second input, repoints the audio mapping at
// it, and hands the result to the real transcoder. Everything else is passed
// through untouched.
//
// Two hard rules, both learned by breaking them:
//
//   - Nothing may be written to stdout. Plex reads the transcoder's output, and
//     anything of ours in that stream is parsed as transcoder output. The log
//     goes to a file.
//   - The real transcoder must inherit our own stdout and stderr handles rather
//     than pipes we then copy, and it must die when we do. Plex kills the
//     process it spawned - us - and without a job object the transcoder
//     underneath carries on encoding into nothing.
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/WkdXeqtr/plex-external-audio/internal/plex"
)

// wrapperMarker is how the guard tells this binary from the real transcoder.
//
// It is compared byte for byte, never hashed: a hash answers "is this the build
// I recorded", which is a different question and gets the wrong answer after
// every rebuild. That mistake once cost a real Plex Transcoder.exe, parked as
// the "original" because the guard no longer recognised its own previous build.
//
// The string spells the project's former name and MUST NOT be changed. An
// installed copy carries the old marker, and a guard that stopped recognising
// it would treat the wrapper as a genuine Plex binary and park it over the real
// original - destroying it. The name is internal and never shown to anyone.
const wrapperMarker = "PLEX-CUSTOM-AUDIO-WRAPPER-MARKER-e9f1c0a4-DO-NOT-PARK-THIS-FILE"

func main() {
	logTo(logPath())
	log.Printf("--- wrapper %s ---", wrapperMarker[:24])

	args := os.Args[1:]
	rewritten, err := rewrite(args)
	if err != nil {
		// Not fatal by design. If anything about the rewrite is unclear we run
		// the original command unchanged: the external track will not play, but
		// everything else on the server keeps working.
		log.Printf("passing the command through unchanged: %v", err)
		rewritten = args
	}

	original, err := originalPath()
	if err != nil {
		log.Fatalf("cannot find the real transcoder: %v", err)
	}
	run(original, rewritten)
}

// --- locating the real transcoder -------------------------------------------

// originalPath is our own path with "_org" before the extension.
//
// Deriving it from our own name rather than hardcoding "Plex Transcoder_org.exe"
// keeps the two halves in step: whatever Plex called the binary it launched,
// the parked original sits next to it under the same name.
func originalPath() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	ext := filepath.Ext(self)
	parked := strings.TrimSuffix(self, ext) + "_org" + ext
	if _, err := os.Stat(parked); err != nil {
		return "", fmt.Errorf("%s: %w", parked, err)
	}
	return parked, nil
}

// --- rewriting the command line ---------------------------------------------

// streamRef matches a reference to a stream of the first input, as written
// inside -filter_complex: [0:1001]. The brackets are part of ffmpeg's filter
// syntax, not of the index.
var streamRef = regexp.MustCompile(`\[0:(\d+)\]`)

// mapRef matches the argument of -map when it addresses a stream directly:
// 0:1001, with or without brackets.
var mapRef = regexp.MustCompile(`^\[?0:(\d+)\]?$`)

// rewrite turns a command line addressing an external stream into one that
// addresses a second input file.
//
// The shape it is working with looks like this, with the parts that matter
// spaced out:
//
//	... -i VIDEO ... -filter_complex "[0:1001] aresample=...[2]" -map "[2]" ...
//
// and it becomes
//
//	... -i VIDEO -analyzeduration N -probesize N -i AUDIO ...
//	    -filter_complex "[1:0] aresample=...[2]" -map "[2]" ...
//
// Returning an error means "leave the command alone", not "fail" - see main.
func rewrite(args []string) ([]string, error) {
	videoIdx := indexOfInput(args)
	if videoIdx < 0 {
		return nil, fmt.Errorf("no -i in the command line")
	}
	video := args[videoIdx+1]

	index, ok := externalIndex(args)
	if !ok {
		// The overwhelmingly common case: Plex wants a track that really is
		// inside the file. Nothing for us to do.
		log.Printf("no external stream requested, video: %s", video)
		return args, nil
	}
	log.Printf("external stream %d requested for %s", index, video)

	dbPath, err := plex.DefaultDBPath()
	if err != nil {
		return nil, err
	}
	db, err := plex.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	ext, err := db.ExternalFor(video, index)
	if err != nil {
		return nil, fmt.Errorf("stream %d of %s is not in the database: %w", index, video, err)
	}
	if _, err := os.Stat(ext.Path); err != nil {
		return nil, fmt.Errorf("audio file is gone: %w", err)
	}
	log.Printf("audio file: %s (stream %d inside it)", ext.Path, ext.SubIndex)

	// The audio file becomes input 1, and every reference to stream `index` of
	// input 0 becomes a reference to the right stream of input 1.
	from := fmt.Sprintf("[0:%d]", index)
	to := fmt.Sprintf("[1:%d]", ext.SubIndex)

	out := make([]string, 0, len(args)+6)
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case i == videoIdx+1:
			// the video path itself, followed by the new input
			out = append(out, a,
				"-analyzeduration", "20000000",
				"-probesize", "20000000",
				"-i", ext.Path)
		case i > 0 && args[i-1] == "-map":
			if m := mapRef.FindStringSubmatch(a); m != nil && m[1] == strconv.Itoa(index) {
				out = append(out, fmt.Sprintf("1:%d", ext.SubIndex))
				continue
			}
			out = append(out, a)
		case strings.Contains(a, from):
			out = append(out, strings.ReplaceAll(a, from, to))
		default:
			out = append(out, a)
		}
	}
	return out, nil
}

// indexOfInput returns the position of the first -i, or -1.
func indexOfInput(args []string) int {
	for i, a := range args {
		if a == "-i" && i+1 < len(args) {
			return i
		}
	}
	return -1
}

// externalIndex finds the stream index Plex is asking for, if it is one of ours.
//
// Plex addresses the audio in one of two ways depending on what it is doing to
// it: through a filter graph, where the reference appears as [0:1001] inside
// -filter_complex, or straight through -map 0:1001 when no filtering is needed.
// Both have to be recognised, and both appear in real logs.
func externalIndex(args []string) (int, bool) {
	for i, a := range args {
		for _, m := range streamRef.FindAllStringSubmatch(a, -1) {
			if n, err := strconv.Atoi(m[1]); err == nil && n >= plex.ExternalIndexBase {
				return n, true
			}
		}
		if i > 0 && args[i-1] == "-map" {
			if m := mapRef.FindStringSubmatch(a); m != nil {
				if n, err := strconv.Atoi(m[1]); err == nil && n >= plex.ExternalIndexBase {
					return n, true
				}
			}
		}
	}
	return 0, false
}

// --- running the real thing --------------------------------------------------

func run(original string, args []string) {
	log.Printf("running %s with %d arguments", original, len(args))

	cmd := exec.Command(original, args...)
	// Our own handles, not pipes. Plex reads this output directly, and putting
	// a copy loop in between changes the timing it depends on.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		log.Fatalf("cannot start %s: %v", original, err)
	}
	// Tie it to us before waiting: Plex kills the process it spawned, and an
	// untethered transcoder would keep encoding with nobody listening.
	tieToUs(cmd.Process.Pid)

	err := cmd.Wait()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			log.Printf("waiting for the transcoder: %v", err)
			code = 1
		}
	}
	log.Printf("--- transcoder exited with %d ---", code)
	os.Exit(code)
}

// --- logging -----------------------------------------------------------------

func logPath() string {
	return filepath.Join(os.TempDir(), "plex-external-audio.log")
}

// logTo sends the log to a file and nowhere else.
//
// Deliberately not to stderr as well: stderr belongs to the real transcoder,
// and Plex parses what comes out of it.
func logTo(path string) {
	log.SetFlags(log.LstdFlags)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.SetOutput(discard{})
		return
	}
	log.SetOutput(f)
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
