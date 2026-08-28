package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// missingExecutable is a name that cannot possibly resolve through
// PATH. Tests that only care about the plumbing around ffprobe use it
// so they fail fast and run on a machine where ffprobe is not
// installed at all.
const missingExecutable = "plex-external-audio-no-such-ffprobe"

// typicalReport is the sort of output ffprobe gives for a Matroska file
// with one video and one audio stream.
const typicalReport = `{
    "streams": [
        {
            "index": 0,
            "codec_name": "h264",
            "codec_type": "video",
            "profile": "High",
            "width": 1920,
            "height": 1080
        },
        {
            "index": 1,
            "codec_name": "eac3",
            "codec_type": "audio",
            "profile": "unknown",
            "sample_rate": "48000",
            "channels": 6,
            "channel_layout": "5.1(side)",
            "bit_rate": "640000",
            "tags": {
                "language": "rus",
                "title": "Dubbed"
            }
        }
    ]
}`

func TestParseStreamsReadsEveryField(t *testing.T) {
	streams, err := parseStreams([]byte(typicalReport))
	if err != nil {
		t.Fatalf("parseStreams returned an error: %v", err)
	}
	if len(streams) != 2 {
		t.Fatalf("got %d streams, want 2", len(streams))
	}

	video := streams[0]
	if video.Index != 0 || video.CodecType != "video" ||
		video.CodecName != "h264" || video.Profile != "High" {
		t.Errorf("video stream parsed as %+v", video)
	}
	if video.Channels != 0 || video.SampleRate != "" {
		t.Errorf("video stream invented audio fields: %+v", video)
	}

	audio := streams[1]
	if audio.Index != 1 {
		t.Errorf("Index = %d, want 1", audio.Index)
	}
	if audio.CodecName != "eac3" {
		t.Errorf("CodecName = %q, want %q", audio.CodecName, "eac3")
	}
	if audio.CodecType != "audio" {
		t.Errorf("CodecType = %q, want %q", audio.CodecType, "audio")
	}
	if audio.Profile != "unknown" {
		t.Errorf("Profile = %q, want %q", audio.Profile, "unknown")
	}
	if audio.Channels != 6 {
		t.Errorf("Channels = %d, want 6", audio.Channels)
	}
	if audio.ChannelLayout != "5.1(side)" {
		t.Errorf("ChannelLayout = %q, want %q",
			audio.ChannelLayout, "5.1(side)")
	}
	// The numeric looking fields have to survive as text, because that
	// is what the rest of the program stores.
	if audio.SampleRate != "48000" {
		t.Errorf("SampleRate = %q, want %q", audio.SampleRate, "48000")
	}
	if audio.BitRate != "640000" {
		t.Errorf("BitRate = %q, want %q", audio.BitRate, "640000")
	}
	if audio.Tags["language"] != "rus" {
		t.Errorf("language tag = %q, want %q",
			audio.Tags["language"], "rus")
	}
	if audio.Tags["title"] != "Dubbed" {
		t.Errorf("title tag = %q, want %q",
			audio.Tags["title"], "Dubbed")
	}
}

func TestParseStreamsFoldsTagKeysToLowerCase(t *testing.T) {
	const report = `{
        "streams": [
            {
                "index": 2,
                "codec_type": "audio",
                "tags": {
                    "LANGUAGE": "ukr",
                    "TITLE": "Ukrainian dub",
                    "BPS-eng": "640000"
                }
            }
        ]
    }`

	streams, err := parseStreams([]byte(report))
	if err != nil {
		t.Fatalf("parseStreams returned an error: %v", err)
	}
	if len(streams) != 1 {
		t.Fatalf("got %d streams, want 1", len(streams))
	}

	tags := streams[0].Tags
	if tags["language"] != "ukr" {
		t.Errorf("language tag = %q, want %q", tags["language"], "ukr")
	}
	if tags["title"] != "Ukrainian dub" {
		t.Errorf("title tag = %q, want %q",
			tags["title"], "Ukrainian dub")
	}
	if tags["bps-eng"] != "640000" {
		t.Errorf("bps-eng tag = %q, want %q",
			tags["bps-eng"], "640000")
	}
	for name := range tags {
		if name != strings.ToLower(name) {
			t.Errorf("tag key %q was left in mixed case", name)
		}
	}
}

func TestParseStreamsKeepsFirstNonEmptyDuplicateTag(t *testing.T) {
	// A remuxed file can carry the same tag under two spellings, and
	// one of them is often an empty leftover.
	const report = `{
        "streams": [
            {
                "index": 0,
                "codec_type": "audio",
                "tags": {
                    "LANGUAGE": "jpn",
                    "language": "",
                    "Title": "Original",
                    "TITLE": "Ignored duplicate"
                }
            }
        ]
    }`

	streams, err := parseStreams([]byte(report))
	if err != nil {
		t.Fatalf("parseStreams returned an error: %v", err)
	}
	tags := streams[0].Tags
	if tags["language"] != "jpn" {
		t.Errorf("language tag = %q, want %q", tags["language"], "jpn")
	}
	if tags["title"] != "Original" {
		t.Errorf("title tag = %q, want %q", tags["title"], "Original")
	}
}

func TestParseStreamsWithoutTags(t *testing.T) {
	for name, report := range map[string]string{
		"no tags key": `{"streams": [
            {"index": 0, "codec_name": "flac", "codec_type": "audio"}
        ]}`,
		"empty tags": `{"streams": [
            {"index": 0, "codec_type": "audio", "tags": {}}
        ]}`,
		"null tags": `{"streams": [
            {"index": 0, "codec_type": "audio", "tags": null}
        ]}`,
	} {
		streams, err := parseStreams([]byte(report))
		if err != nil {
			t.Errorf("%s: parseStreams returned an error: %v",
				name, err)
			continue
		}
		if len(streams) != 1 {
			t.Errorf("%s: got %d streams, want 1", name, len(streams))
			continue
		}
		if len(streams[0].Tags) != 0 {
			t.Errorf("%s: Tags = %v, want it empty",
				name, streams[0].Tags)
		}
		// Reading from a nil map is legal, and callers lean on that
		// rather than on the map having been created.
		if language := streams[0].Tags["language"]; language != "" {
			t.Errorf("%s: language tag = %q, want it empty",
				name, language)
		}
	}
}

func TestParseStreamsAcceptsNumericFields(t *testing.T) {
	// Some builds and forks write these as numbers instead of text,
	// and ffprobe itself does so for a profile it has no name for.
	const report = `{
        "streams": [
            {
                "index": 0,
                "codec_name": "pcm_s24le",
                "codec_type": "audio",
                "profile": -99,
                "sample_rate": 96000,
                "channels": 2,
                "bit_rate": 4608000,
                "tags": {"language": "eng"}
            }
        ]
    }`

	streams, err := parseStreams([]byte(report))
	if err != nil {
		t.Fatalf("parseStreams returned an error: %v", err)
	}
	stream := streams[0]
	if stream.SampleRate != "96000" {
		t.Errorf("SampleRate = %q, want %q", stream.SampleRate, "96000")
	}
	if stream.BitRate != "4608000" {
		t.Errorf("BitRate = %q, want %q", stream.BitRate, "4608000")
	}
	if stream.Profile != "-99" {
		t.Errorf("Profile = %q, want %q", stream.Profile, "-99")
	}
}

func TestParseStreamsDropsNotAvailablePlaceholder(t *testing.T) {
	const report = `{
        "streams": [
            {
                "index": 0,
                "codec_name": "aac",
                "codec_type": "audio",
                "profile": "N/A",
                "sample_rate": "44100",
                "bit_rate": "N/A",
                "channel_layout": "N/A",
                "tags": {"title": "N/A"}
            }
        ]
    }`

	streams, err := parseStreams([]byte(report))
	if err != nil {
		t.Fatalf("parseStreams returned an error: %v", err)
	}
	stream := streams[0]
	if stream.BitRate != "" {
		t.Errorf("BitRate = %q, want it empty", stream.BitRate)
	}
	if stream.Profile != "" {
		t.Errorf("Profile = %q, want it empty", stream.Profile)
	}
	if stream.ChannelLayout != "" {
		t.Errorf("ChannelLayout = %q, want it empty",
			stream.ChannelLayout)
	}
	if stream.Tags["title"] != "" {
		t.Errorf("title tag = %q, want it empty", stream.Tags["title"])
	}
	if stream.SampleRate != "44100" {
		t.Errorf("SampleRate = %q, want it untouched", stream.SampleRate)
	}
}

func TestParseStreamsWithoutAnyStreams(t *testing.T) {
	for name, report := range map[string]string{
		"empty list": `{"streams": []}`,
		"no key":     `{}`,
	} {
		streams, err := parseStreams([]byte(report))
		if err != nil {
			t.Errorf("%s: parseStreams returned an error: %v",
				name, err)
			continue
		}
		if len(streams) != 0 {
			t.Errorf("%s: got %d streams, want none", name,
				len(streams))
		}
	}
}

func TestParseStreamsRejectsBadInput(t *testing.T) {
	for name, report := range map[string]string{
		"nothing":       "",
		"blank":         "   \n\t ",
		"truncated":     `{"streams": [{"index": 0`,
		"not an object": `["streams"]`,
		"wrong type":    `{"streams": {"index": 0}}`,
	} {
		if _, err := parseStreams([]byte(report)); err == nil {
			t.Errorf("%s: parseStreams accepted %q", name, report)
		}
	}
}

func TestManyOnEmptyInput(t *testing.T) {
	prober := Prober{Exe: missingExecutable}

	for name, paths := range map[string][]string{
		"nil":   nil,
		"empty": {},
	} {
		results := prober.Many(context.Background(), paths, 4)
		if results == nil {
			t.Errorf("%s: Many returned a nil map", name)
			continue
		}
		if len(results) != 0 {
			t.Errorf("%s: Many returned %d results, want none",
				name, len(results))
		}
	}
}

func TestManyProbesEachPathOnce(t *testing.T) {
	// The executable does not exist, so every probe fails immediately.
	// What matters here is the bookkeeping: duplicates collapse, every
	// distinct path gets a Result, and one failure does not swallow
	// the others.
	prober := Prober{Exe: missingExecutable}
	paths := []string{
		"first.mka",
		"second.mka",
		"first.mka",
		"third.mka",
		"second.mka",
		"first.mka",
	}

	results := prober.Many(context.Background(), paths, 3)
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3: %v", len(results), results)
	}
	for _, path := range paths {
		result, found := results[path]
		if !found {
			t.Fatalf("no result for %q", path)
		}
		if result.Path != path {
			t.Errorf("Result.Path = %q, want %q", result.Path, path)
		}
		if result.Err == nil {
			t.Errorf("%q: Err is nil although ffprobe is missing",
				path)
		}
		if result.Streams != nil {
			t.Errorf("%q: Streams = %v, want nil on failure",
				path, result.Streams)
		}
	}
}

func TestManyDefaultsAndClampsWorkerCount(t *testing.T) {
	prober := Prober{Exe: missingExecutable}
	paths := []string{"one.mka", "two.mka"}

	// Zero and negative counts mean "decide for me", and asking for
	// far more workers than files must not deadlock or lose results.
	for _, workers := range []int{-7, 0, 1, 64} {
		results := prober.Many(context.Background(), paths, workers)
		if len(results) != len(paths) {
			t.Errorf("workers=%d: got %d results, want %d",
				workers, len(results), len(paths))
		}
	}
}

func TestManyOnCancelledContext(t *testing.T) {
	prober := Prober{Exe: missingExecutable}
	paths := []string{"a.mka", "b.mka", "c.mka", "d.mka"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// A cancelled context must neither hang nor leave a path without
	// an answer, so the call is watched by a timer.
	done := make(chan map[string]Result, 1)
	go func() {
		done <- prober.Many(ctx, paths, 2)
	}()

	select {
	case results := <-done:
		if len(results) != len(paths) {
			t.Fatalf("got %d results, want %d",
				len(results), len(paths))
		}
		for _, path := range paths {
			result := results[path]
			if result.Err == nil {
				t.Errorf("%q: Err is nil although the context was "+
					"cancelled", path)
			}
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Many did not return on a cancelled context")
	}
}

func TestOneOnCancelledContext(t *testing.T) {
	prober := Prober{Exe: missingExecutable}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	streams, err := prober.One(ctx, "movie.mka")
	if err == nil {
		t.Fatal("One accepted a cancelled context")
	}
	if streams != nil {
		t.Errorf("Streams = %v, want nil", streams)
	}
	if !strings.Contains(err.Error(), "movie.mka") {
		t.Errorf("error %q does not name the file", err)
	}
}

func TestOneReportsMissingExecutable(t *testing.T) {
	prober := Prober{Exe: missingExecutable}

	_, err := prober.One(context.Background(), "movie.mka")
	if err == nil {
		t.Fatal("One succeeded although ffprobe is missing")
	}
	if !strings.Contains(err.Error(), "movie.mka") {
		t.Errorf("error %q does not name the file", err)
	}
}

func TestCheckReportsMissingExecutable(t *testing.T) {
	err := Prober{Exe: missingExecutable}.Check()
	if err == nil {
		t.Fatal("Check accepted an executable that does not exist")
	}
	if !strings.Contains(err.Error(), missingExecutable) {
		t.Errorf("error %q does not name the executable", err)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error %q does not say the file is missing", err)
	}
}

func TestExecutableFallsBackToPlainName(t *testing.T) {
	for _, configured := range []string{"", "   ", "\t"} {
		name := Prober{Exe: configured}.executable()
		if name != defaultExecutable {
			t.Errorf("Exe=%q resolved to %q, want %q",
				configured, name, defaultExecutable)
		}
	}

	const custom = `C:\ffmpeg\bin\ffprobe.exe`
	if name := (Prober{Exe: custom}).executable(); name != custom {
		t.Errorf("Exe=%q resolved to %q", custom, name)
	}
}

// The tests below run the whole path through an actual child process.
// ffprobe cannot be assumed to be installed on a machine that only
// builds this program, so the test binary stands in for it: TestMain
// notices the environment variable and acts out ffprobe instead of
// running tests.

// fakeProbeRole names the behaviour the stand-in should act out. It
// doubles as the switch that turns the test binary into that stand-in.
const fakeProbeRole = "PLEX_EXTERNAL_AUDIO_FAKE_FFPROBE_ROLE"

const (
	roleWorking     = "working ffprobe"
	roleOtherTool   = "some other ffmpeg tool"
	roleBrokenFile  = "refuses to read the file"
	roleBadOutput   = "prints something that is not json"
	roleSilentCheck = "prints nothing at all"
)

func TestMain(m *testing.M) {
	if role := os.Getenv(fakeProbeRole); role != "" {
		os.Exit(playFakeProbe(role, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// playFakeProbe imitates one ffprobe run and returns the exit code it
// should end with. It also checks the command line, so every test that
// goes through it confirms that One asks ffprobe the expected question.
func playFakeProbe(role string, arguments []string) int {
	if len(arguments) == 1 && arguments[0] == "-version" {
		switch role {
		case roleOtherTool:
			fmt.Println("ffmpeg version 6.1.1 Copyright (c) 2000-2023" +
				" the FFmpeg developers")
		case roleSilentCheck:
		default:
			fmt.Println("ffprobe version 6.1.1 Copyright (c) 2007-2023" +
				" the FFmpeg developers")
		}
		return 0
	}

	// Anything else has to be the exact command line One documents,
	// because a real ffprobe would not understand a different one.
	if len(arguments) != 6 || arguments[0] != "-v" ||
		arguments[1] != "error" || arguments[2] != "-show_streams" ||
		arguments[3] != "-of" || arguments[4] != "json" {
		fmt.Fprintf(os.Stderr, "unexpected command line %q", arguments)
		return 3
	}
	path := arguments[5]

	switch role {
	case roleBrokenFile:
		fmt.Fprintf(os.Stderr,
			"%s: Invalid data found when processing input\n", path)
		return 1
	case roleBadOutput:
		fmt.Println("this line is not json at all")
		return 0
	}

	// The path is echoed back as the stream title so that a caller can
	// prove which file a given report belongs to.
	report := map[string]any{
		"streams": []map[string]any{{
			"index":          0,
			"codec_name":     "eac3",
			"codec_type":     "audio",
			"sample_rate":    "48000",
			"channels":       6,
			"channel_layout": "5.1(side)",
			"bit_rate":       "640000",
			"tags":           map[string]string{"TITLE": path},
		}},
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		fmt.Fprintf(os.Stderr, "encoding the fake report: %v", err)
		return 4
	}
	os.Stdout.Write(encoded)
	return 0
}

// fakeProber turns the test binary into ffprobe for the current test
// and returns a Prober aimed at it.
func fakeProber(t *testing.T, role string) Prober {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary: %v", err)
	}
	t.Setenv(fakeProbeRole, role)
	return Prober{Exe: executable}
}

func TestOneReadsWhatFfprobePrints(t *testing.T) {
	prober := fakeProber(t, roleWorking)
	const path = `C:\media\Movie (2019)\Movie.mka`

	streams, err := prober.One(context.Background(), path)
	if err != nil {
		t.Fatalf("One returned an error: %v", err)
	}
	if len(streams) != 1 {
		t.Fatalf("got %d streams, want 1", len(streams))
	}
	if streams[0].CodecName != "eac3" || streams[0].Channels != 6 {
		t.Errorf("stream parsed as %+v", streams[0])
	}
	// The stand-in echoes the path back as the title, which proves the
	// file name survived the trip through the command line intact.
	if streams[0].Tags["title"] != path {
		t.Errorf("title tag = %q, want %q",
			streams[0].Tags["title"], path)
	}
}

func TestOneReportsWhatFfprobeComplainedAbout(t *testing.T) {
	prober := fakeProber(t, roleBrokenFile)

	_, err := prober.One(context.Background(), "broken.mka")
	if err == nil {
		t.Fatal("One ignored a failing ffprobe")
	}
	if !strings.Contains(err.Error(), "broken.mka") {
		t.Errorf("error %q does not name the file", err)
	}
	if !strings.Contains(err.Error(), "Invalid data found") {
		t.Errorf("error %q drops the ffprobe complaint", err)
	}
}

func TestOneRejectsOutputThatIsNotJSON(t *testing.T) {
	prober := fakeProber(t, roleBadOutput)

	_, err := prober.One(context.Background(), "odd.mka")
	if err == nil {
		t.Fatal("One accepted output that is not json")
	}
	if !strings.Contains(err.Error(), "odd.mka") {
		t.Errorf("error %q does not name the file", err)
	}
}

func TestManyKeepsEachReportWithItsOwnPath(t *testing.T) {
	prober := fakeProber(t, roleWorking)
	paths := []string{
		`C:\media\A.mka`,
		`C:\media\B.mka`,
		`C:\media\C.mka`,
		`C:\media\A.mka`,
		`C:\media\D.mka`,
	}

	results := prober.Many(context.Background(), paths, 3)
	if len(results) != 4 {
		t.Fatalf("got %d results, want 4", len(results))
	}
	for path, result := range results {
		if result.Err != nil {
			t.Errorf("%q: %v", path, result.Err)
			continue
		}
		if len(result.Streams) != 1 {
			t.Errorf("%q: got %d streams, want 1",
				path, len(result.Streams))
			continue
		}
		// Workers run concurrently, so this is where a mixed up slot
		// would show: the report would carry someone else's path.
		if title := result.Streams[0].Tags["title"]; title != path {
			t.Errorf("result for %q carries the report of %q",
				path, title)
		}
	}
}

func TestCheckAcceptsRealFfprobe(t *testing.T) {
	prober := fakeProber(t, roleWorking)
	if err := prober.Check(); err != nil {
		t.Fatalf("Check rejected ffprobe: %v", err)
	}
}

func TestCheckRejectsAnotherFfmpegTool(t *testing.T) {
	prober := fakeProber(t, roleOtherTool)

	err := prober.Check()
	if err == nil {
		t.Fatal("Check accepted ffmpeg as if it were ffprobe")
	}
	if !strings.Contains(err.Error(), "ffmpeg version") {
		t.Errorf("error %q does not show what answered instead", err)
	}
}

func TestCheckRejectsSilentExecutable(t *testing.T) {
	prober := fakeProber(t, roleSilentCheck)
	if err := prober.Check(); err == nil {
		t.Fatal("Check accepted an executable that printed nothing")
	}
}

func TestCappedBufferKeepsOnlyTheBeginning(t *testing.T) {
	buffer := &cappedBuffer{limit: 10}
	written, err := buffer.Write([]byte("first line\nsecond line\n"))
	if err != nil {
		t.Fatalf("Write returned an error: %v", err)
	}
	if written != len("first line\nsecond line\n") {
		t.Errorf("Write reported %d bytes, want the whole slice",
			written)
	}
	if len(buffer.kept) != 10 {
		t.Errorf("kept %d bytes, want 10", len(buffer.kept))
	}
	text := buffer.text()
	if !strings.HasPrefix(text, "first line") {
		t.Errorf("text() = %q, want it to start with the first line",
			text)
	}
	if !strings.HasSuffix(text, "...") {
		t.Errorf("text() = %q, want it marked as cut short", text)
	}
	if strings.Contains(text, "\n") {
		t.Errorf("text() = %q, want a single line", text)
	}
}
