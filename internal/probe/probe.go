// Package probe wraps the ffprobe command line tool.
//
// Plex keeps one database row per audio stream, and filling such a row
// needs the codec, the channel count and layout, the sample rate, the
// bit rate, the language and the title of that stream. ffprobe is the
// only dependable source for those values, so this package runs it and
// turns its JSON report into plain Go values.
//
// The package stays platform neutral on purpose. Hiding the console
// window that a child process pops up on Windows needs syscall level
// process attributes, which would drag build tags and Win32 knowledge
// in here; that decision belongs to the program that links this
// package, not to the package itself.
package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Stream is one elementary stream as ffprobe describes it. Only the
// fields that end up in the Plex database are kept; everything else in
// the ffprobe report is ignored.
type Stream struct {
	// Index is the position of the stream inside its container, which
	// is also how the transcoder addresses it later on.
	Index int
	// CodecName is the short ffmpeg name, for example "eac3" or "dts".
	CodecName string
	// CodecType is "audio", "video", "subtitle" and so on. Callers are
	// expected to filter on it; this package returns every stream.
	CodecType string
	// Profile is the codec profile, such as "DTS-HD MA". It is empty
	// when ffprobe does not know one.
	Profile string
	// Channels is the channel count, zero when unknown.
	Channels int
	// ChannelLayout is the layout name, for example "stereo" or
	// "5.1(side)". It is empty for layouts ffmpeg cannot name.
	ChannelLayout string
	// SampleRate is in hertz. It stays text because that is how
	// ffprobe reports it and how Plex stores it.
	SampleRate string
	// BitRate is in bits per second, empty when the container does not
	// record one.
	BitRate string
	// Tags holds the stream metadata with every key folded to lower
	// case, so "language" and "title" are always reachable under those
	// exact names. It is empty when the stream carries no metadata.
	Tags map[string]string
}

// Prober runs ffprobe. The zero value is usable and looks the binary up
// in PATH under its usual name.
type Prober struct {
	// Exe is the ffprobe binary: either a bare name resolved through
	// PATH or a full path. An empty value means plain "ffprobe".
	Exe string
}

// Result is everything Many learned about a single file. Streams is nil
// whenever Err is set.
type Result struct {
	Path    string
	Streams []Stream
	Err     error
}

const (
	// defaultExecutable is what an empty Prober.Exe falls back to.
	defaultExecutable = "ffprobe"

	// checkTimeout bounds Check. A binary that is not ffprobe may well
	// sit and wait for input instead of printing a version, and Check
	// has no context of its own to cut that short.
	checkTimeout = 15 * time.Second

	// commandWaitDelay bounds the wait for the output pipes after a
	// process has been killed. Without it a child that handed the pipe
	// to a grandchild could keep Wait blocked forever, which is exactly
	// the hang that context cancellation is supposed to prevent.
	commandWaitDelay = 5 * time.Second

	// stderrLimit is how much of the ffprobe complaint is kept for the
	// error message. A badly broken file can produce a lot of it and
	// none of the rest is worth holding in memory.
	stderrLimit = 8 << 10
)

// executable resolves the binary to run.
func (p Prober) executable() string {
	if strings.TrimSpace(p.Exe) == "" {
		return defaultExecutable
	}
	return p.Exe
}

// Check makes sure the configured binary exists and really is ffprobe.
// It is meant to run once at startup so a misconfigured path is
// reported as a clear message instead of failing later on every single
// file.
func (p Prober) Check() error {
	executable := p.executable()

	ctx, cancel := context.WithTimeout(
		context.Background(), checkTimeout)
	defer cancel()

	// Stdin is deliberately left closed: a wrong binary that expects
	// input then reaches end of file and gives up quickly.
	command := exec.CommandContext(ctx, executable, "-version")
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	command.WaitDelay = commandWaitDelay

	if err := command.Run(); err != nil {
		switch {
		case errors.Is(err, exec.ErrNotFound),
			errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf(
				"ffprobe executable %q was not found: %w",
				executable, err)
		case ctx.Err() != nil:
			return fmt.Errorf(
				"%q did not answer -version within %s: %w",
				executable, checkTimeout, ctx.Err())
		default:
			return fmt.Errorf("running %q -version: %w",
				executable, err)
		}
	}

	// ffprobe greets with a line such as "ffprobe version 6.1.1
	// Copyright (c) 2007-2023 the FFmpeg developers". ffmpeg and
	// ffplay answer -version too and would otherwise pass this check,
	// so the tool name itself has to be there.
	greeting := firstLine(output.String())
	if !strings.Contains(strings.ToLower(greeting), "ffprobe") {
		if greeting == "" {
			return fmt.Errorf(
				"%q answered -version with no output at all, "+
					"it does not look like ffprobe", executable)
		}
		return fmt.Errorf(
			"%q does not look like ffprobe, it answered "+
				"-version with %q", executable, greeting)
	}
	return nil
}

// One probes a single file and returns every stream it contains, video
// and subtitle streams included. Filtering by Stream.CodecType is left
// to the caller.
func (p Prober) One(ctx context.Context, path string) ([]Stream, error) {
	// Starting a process for a context that is already gone would be
	// pure waste, so the cheap check comes first.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("probing %q: %w", path, err)
	}

	// "-v error" silences the banner and the informational chatter, so
	// whatever is left on stderr is a genuine complaint about the file.
	command := exec.CommandContext(ctx, p.executable(),
		"-v", "error", "-show_streams", "-of", "json", path)
	var stdout bytes.Buffer
	stderr := &cappedBuffer{limit: stderrLimit}
	command.Stdout = &stdout
	command.Stderr = stderr
	command.WaitDelay = commandWaitDelay

	if err := command.Run(); err != nil {
		// A killed process reports a plain "signal: killed" style
		// failure, which hides the real reason, so cancellation is
		// reported as itself.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("probing %q: %w", path, ctxErr)
		}
		if detail := stderr.text(); detail != "" {
			return nil, fmt.Errorf("ffprobe on %q: %w: %s",
				path, err, detail)
		}
		return nil, fmt.Errorf("ffprobe on %q: %w", path, err)
	}

	streams, err := parseStreams(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("reading ffprobe report for %q: %w",
			path, err)
	}
	return streams, nil
}

// Many probes every path with a pool of workers and returns exactly one
// Result per distinct input path, keyed by that path. A file that fails
// only spoils its own Result: the others are probed regardless.
//
// A pool is what makes this usable at all. ffprobe cannot be told to
// look at several files in one run, so a library of a couple of
// thousand tracks means a couple of thousand short lived processes, and
// running them one after another wastes most of the wall clock time on
// process startup rather than on reading files.
//
// workers is the number of processes allowed to run at once; anything
// less than one means runtime.NumCPU().
func (p Prober) Many(
	ctx context.Context, paths []string, workers int,
) map[string]Result {
	// The same file can easily be listed twice, for instance when a
	// caller collects paths from several directories, and probing it
	// twice would only burn a process.
	unique := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if _, duplicate := seen[path]; duplicate {
			continue
		}
		seen[path] = struct{}{}
		unique = append(unique, path)
	}

	results := make(map[string]Result, len(unique))
	if len(unique) == 0 {
		return results
	}

	if workers < 1 {
		workers = runtime.NumCPU()
	}
	// More workers than files would just be idle goroutines.
	if workers > len(unique) {
		workers = len(unique)
	}

	// Each worker owns the slot it pulled off the queue, so the slice
	// needs no lock: exactly one goroutine writes each element and
	// nothing reads them until every worker has finished.
	collected := make([]Result, len(unique))
	queue := make(chan int)
	var waitGroup sync.WaitGroup

	waitGroup.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer waitGroup.Done()
			for index := range queue {
				path := unique[index]
				// Once the context is gone no further processes may
				// start, but the promise of one Result per path still
				// stands, so the rest of the queue is drained with the
				// cancellation error rather than left unanswered.
				if err := ctx.Err(); err != nil {
					collected[index] = Result{
						Path: path,
						Err: fmt.Errorf("probing %q: %w",
							path, err),
					}
					continue
				}
				streams, err := p.One(ctx, path)
				collected[index] = Result{
					Path:    path,
					Streams: streams,
					Err:     err,
				}
			}
		}()
	}

	// Feeding the queue cannot block forever: the workers keep draining
	// it even after cancellation.
	for index := range unique {
		queue <- index
	}
	close(queue)
	waitGroup.Wait()

	for _, result := range collected {
		results[result.Path] = result
	}
	return results
}

// parseStreams turns the raw output of "ffprobe -of json" into streams.
// It is a separate function so that all the awkward parts of the format
// can be tested without ffprobe being installed.
func parseStreams(data []byte) ([]Stream, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("ffprobe produced no output")
	}

	var report struct {
		Streams []rawStream `json:"streams"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("decoding ffprobe json: %w", err)
	}

	// A file with no streams at all is odd but not an error, and an
	// empty slice is friendlier to callers than nil.
	streams := make([]Stream, 0, len(report.Streams))
	for _, raw := range report.Streams {
		streams = append(streams, raw.stream())
	}
	return streams, nil
}

// rawStream mirrors the wire format. It is kept apart from Stream so
// that the exported type stays free of json tags and of the helper
// types that the quirks below require.
type rawStream struct {
	Index         int        `json:"index"`
	CodecName     string     `json:"codec_name"`
	CodecType     string     `json:"codec_type"`
	Profile       looseText  `json:"profile"`
	Channels      int        `json:"channels"`
	ChannelLayout string     `json:"channel_layout"`
	SampleRate    looseText  `json:"sample_rate"`
	BitRate       looseText  `json:"bit_rate"`
	Tags          taggedText `json:"tags"`
}

// stream converts the wire form into the exported one.
func (r rawStream) stream() Stream {
	return Stream{
		Index:         r.Index,
		CodecName:     cleanValue(r.CodecName),
		CodecType:     cleanValue(r.CodecType),
		Profile:       cleanValue(string(r.Profile)),
		Channels:      r.Channels,
		ChannelLayout: cleanValue(r.ChannelLayout),
		SampleRate:    cleanValue(string(r.SampleRate)),
		BitRate:       cleanValue(string(r.BitRate)),
		Tags:          map[string]string(r.Tags),
	}
}

// looseText is a string that also accepts a JSON number or boolean.
//
// ffprobe writes sample_rate and bit_rate as text even though they are
// numbers, but it is not consistent everywhere: profile, for one, comes
// out as a bare number whenever the codec has no name for it. Accepting
// both spellings keeps one unusual stream from failing an entire file.
type looseText string

// UnmarshalJSON implements json.Unmarshaler.
func (t *looseText) UnmarshalJSON(data []byte) error {
	// UseNumber keeps long values such as a bit rate exactly as they
	// were written instead of routing them through a float.
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	switch typed := value.(type) {
	case nil:
		*t = ""
	case string:
		*t = looseText(typed)
	case json.Number:
		*t = looseText(typed.String())
	case bool:
		*t = looseText(strconv.FormatBool(typed))
	default:
		return fmt.Errorf(
			"expected text or a number, got %T", value)
	}
	return nil
}

// taggedText is an ffprobe "tags" object with every key folded to lower
// case.
//
// Container formats disagree about capitalisation: Matroska writes
// LANGUAGE and TITLE, MP4 and MPEG-TS write language and title. Folding
// the keys here means the rest of the program looks a tag up once
// instead of trying every spelling.
type taggedText map[string]string

// UnmarshalJSON implements json.Unmarshaler. The object is walked token
// by token rather than decoded into a map, because folding the keys can
// make two of them collide and the choice between them has to be made
// while the original order is still known.
func (t *taggedText) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))

	opening, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("reading tags: %w", err)
	}
	if opening == nil {
		// An explicit null simply means no metadata.
		*t = nil
		return nil
	}
	if delimiter, ok := opening.(json.Delim); !ok ||
		delimiter != '{' {
		return fmt.Errorf("expected a tags object, got %v", opening)
	}

	tags := make(taggedText)
	for decoder.More() {
		nameToken, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("reading tag name: %w", err)
		}
		name, ok := nameToken.(string)
		if !ok {
			return fmt.Errorf("tag name is not text: %v", nameToken)
		}

		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return fmt.Errorf("reading tag %q: %w", name, err)
		}
		var value looseText
		if err := value.UnmarshalJSON(raw); err != nil {
			// A structured tag value is nothing Plex could store, and
			// dropping that one tag is far better than rejecting the
			// whole file over it.
			continue
		}

		folded := strings.ToLower(name)
		cleaned := cleanValue(string(value))
		// A file can carry both LANGUAGE and language. The first
		// non-empty spelling wins, so a later empty duplicate cannot
		// wipe out a value that was already found.
		if existing, found := tags[folded]; found &&
			(existing != "" || cleaned == "") {
			continue
		}
		tags[folded] = cleaned
	}

	// Consume the closing brace so the decoder ends in a sane state.
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("reading end of tags: %w", err)
	}
	*t = tags
	return nil
}

// cleanValue trims a value and drops the placeholder ffprobe uses for
// something it could not determine. Its default writer prints "N/A",
// and some builds let that string through into the JSON output as
// well, where an empty value says the same thing far more usefully.
func cleanValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if strings.EqualFold(trimmed, "N/A") {
		return ""
	}
	return trimmed
}

// firstLine returns the first line of text, without trailing spaces.
func firstLine(text string) string {
	if end := strings.IndexAny(text, "\r\n"); end >= 0 {
		text = text[:end]
	}
	return strings.TrimSpace(text)
}

// cappedBuffer keeps the beginning of what is written to it and only
// counts the rest, so a talkative child process cannot grow the memory
// held for an error message without bound.
type cappedBuffer struct {
	limit   int
	kept    []byte
	written int
}

// Write implements io.Writer and never reports an error, because
// dropping the tail of a diagnostic message must not make the command
// itself look like it failed.
func (b *cappedBuffer) Write(data []byte) (int, error) {
	b.written += len(data)
	if room := b.limit - len(b.kept); room > 0 {
		if len(data) < room {
			room = len(data)
		}
		b.kept = append(b.kept, data[:room]...)
	}
	return len(data), nil
}

// text renders what was kept as a single line, marking the point where
// the message was cut short.
func (b *cappedBuffer) text() string {
	text := strings.TrimSpace(string(b.kept))
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\n", "; ")
	if b.written > len(b.kept) {
		text += " ..."
	}
	return text
}
