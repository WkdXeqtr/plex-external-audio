// Package plex talks to the Plex Media Server library database.
//
// Everything this program does to Plex happens here, and it is deliberately
// narrow. Two rules govern the whole package, both learned the hard way:
//
//   - We write to exactly one table, media_streams. It carries no triggers, no
//     foreign keys and no custom collations, so a plain INSERT from an ordinary
//     SQLite driver is safe. Neighbouring tables are not: metadata_items hangs
//     four FTS4 triggers off a tokenizer only Plex's own build registers, and
//     touching it from anywhere else fails with "unknown tokenizer: collating".
//     We read those tables and never write them.
//
//   - Rows have to look exactly like the ones Plex writes itself. Plex is not
//     defensive about its own data: a value in the wrong shape does not degrade
//     gracefully, it takes the player down. See RepairSelections and the notes
//     on extra_data and language below.
package plex

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	// ExternalIndexBase is where the stream indices of external tracks start.
	//
	// Plex numbers the streams inside a container from zero and passes that
	// number straight to ffmpeg as -map 0:N. No real file has a thousand
	// streams, so anything at or above this is unambiguously ours, which is how
	// both the transcoder wrapper and the cleanup code recognise our rows.
	ExternalIndexBase = 1000

	streamTypeVideo = 1
	streamTypeAudio = 2
)

// DB is an open Plex library database.
type DB struct{ sql *sql.DB }

// Open opens the library database for reading and writing.
//
// The file must already exist. The SQLite driver would happily create an empty
// one, and a typo in a path would then look like a Plex library with nothing in
// it rather than like the mistake it is.
func Open(path string) (*DB, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("plex database not found at %s: %w", path, err)
	}
	if st.IsDir() {
		return nil, fmt.Errorf("plex database path is a directory: %s", path)
	}
	// busy_timeout matters: Plex keeps the database open, and without a wait a
	// concurrent write from the server turns into an instant "database is
	// locked" instead of a short pause.
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(15000)")
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	return &DB{sql: db}, nil
}

func (d *DB) Close() error { return d.sql.Close() }

// DefaultDBPath finds the library database for this machine.
//
// The list is ordered from the most specific to the most generic. On Windows
// the location can be moved, and Plex records the move in the registry rather
// than anywhere in its own data directory - that lookup lives in the
// platform-specific file next to this one.
func DefaultDBPath() (string, error) {
	const leaf = "Plug-in Support/Databases/com.plexapp.plugins.library.db"

	var candidates []string
	if p := registryDataPath(); p != "" {
		candidates = append(candidates, filepath.Join(p, "Plex Media Server", filepath.FromSlash(leaf)))
	}
	if runtime.GOOS == "windows" {
		if p := os.Getenv("LOCALAPPDATA"); p != "" {
			candidates = append(candidates,
				filepath.Join(p, "Plex Media Server", filepath.FromSlash(leaf)))
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, "Library/Application Support/Plex Media Server", leaf))
	}
	candidates = append(candidates,
		// container images published by Plex and by linuxserver.io
		"/config/Library/Application Support/Plex Media Server/"+leaf,
		// distribution packages
		"/var/lib/plexmediaserver/Library/Application Support/Plex Media Server/"+leaf,
		"/usr/local/plexdata/Plex Media Server/"+leaf,
	)

	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("could not find the Plex library database; pass -db with its path")
}

// --- paths ------------------------------------------------------------------

// CleanPath strips the Windows extended-length prefix from a path.
//
// Plex stores some paths as \\?\E:\... . That form is fine for the file APIs
// but rejects forward slashes and confuses anything doing string work on the
// path, so we normalise on the way in. UNC paths are left alone: for them the
// prefix is not decoration, it is part of the syntax.
func CleanPath(p string) string {
	if strings.HasPrefix(p, `\\?\`) && !strings.HasPrefix(p, `\\?\UNC\`) {
		return p[4:]
	}
	return p
}

// FileURL renders a path the way the url column holds it.
//
// This is not a standards-conforming file URI - the drive letter and the
// backslashes are kept verbatim, and nothing is percent-encoded. That is
// deliberate: the value only ever travels between our own two programs, and
// keeping the literal path in it means the wrapper can hand it straight to
// ffmpeg without having to decode anything.
func FileURL(path string) string { return "file://" + CleanPath(path) }

// PathFromURL is the inverse of FileURL.
func PathFromURL(url string) string { return strings.TrimPrefix(url, "file://") }

// --- reading ----------------------------------------------------------------

// Video is one video file in the library.
type Video struct {
	PartID   int64
	ItemID   int64
	Path     string // absolute, extended-length prefix already stripped
	MaxIndex int    // highest stream index on this part, -1 when there are none
	Analysed bool   // false while Plex has not looked inside the file yet
}

// Videos lists every video file Plex knows about.
//
// Parts with no streams at all are reported with Analysed false rather than
// skipped silently: they are not broken, Plex simply has not got to them yet,
// and they will be picked up on a later run once it has.
func (d *DB) Videos() ([]Video, error) {
	rows, err := d.sql.Query(`
		SELECT mp.id, mp.media_item_id, mp.file,
		       IFNULL(MAX(ms."index"), -1)                                   AS max_index,
		       SUM(CASE WHEN ms.stream_type_id = ? THEN 1 ELSE 0 END)        AS video_streams,
		       COUNT(ms.id)                                                  AS all_streams
		FROM media_parts mp
		LEFT JOIN media_streams ms ON ms.media_part_id = mp.id
		WHERE mp.file IS NOT NULL AND mp.file != '' AND mp.deleted_at IS NULL
		GROUP BY mp.id
		HAVING video_streams > 0 OR all_streams = 0`, streamTypeVideo)
	if err != nil {
		return nil, fmt.Errorf("listing video parts: %w", err)
	}
	defer rows.Close()

	var out []Video
	for rows.Next() {
		var v Video
		var videoStreams, allStreams int
		if err := rows.Scan(&v.PartID, &v.ItemID, &v.Path, &v.MaxIndex,
			&videoStreams, &allStreams); err != nil {
			return nil, fmt.Errorf("reading video parts: %w", err)
		}
		v.Path = CleanPath(v.Path)
		v.Analysed = allStreams > 0
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading video parts: %w", err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// External is one audio stream row this program owns.
type External struct {
	ID       int64
	PartID   int64
	ItemID   int64
	Index    int
	Path     string // the audio file on disk
	SubIndex int    // which stream inside that file
}

// Externals lists every row this program has added.
func (d *DB) Externals() ([]External, error) {
	rows, err := d.sql.Query(`
		SELECT id, media_part_id, media_item_id, "index", url, IFNULL(url_index, 0)
		FROM media_streams
		WHERE "index" >= ? AND url IS NOT NULL AND url != ''`, ExternalIndexBase)
	if err != nil {
		return nil, fmt.Errorf("listing external streams: %w", err)
	}
	defer rows.Close()

	var out []External
	for rows.Next() {
		var e External
		var url string
		if err := rows.Scan(&e.ID, &e.PartID, &e.ItemID, &e.Index, &url, &e.SubIndex); err != nil {
			return nil, fmt.Errorf("reading external streams: %w", err)
		}
		e.Path = PathFromURL(url)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ExternalFor finds the audio file behind one stream index of one video.
//
// This is the lookup the transcoder wrapper makes: Plex has asked it for stream
// 1001 of a file, and the answer is which file on disk actually holds it. The
// path is matched both as stored and with the extended-length prefix, because
// which of the two Plex hands the transcoder is not something we control.
func (d *DB) ExternalFor(videoPath string, index int) (External, error) {
	clean := CleanPath(videoPath)
	var e External
	var url string
	err := d.sql.QueryRow(`
		SELECT ms.id, ms.media_part_id, ms.media_item_id, ms."index", ms.url,
		       IFNULL(ms.url_index, 0)
		FROM media_streams ms
		JOIN media_parts mp ON mp.id = ms.media_part_id
		WHERE ms."index" = ? AND (mp.file = ? OR mp.file = ?)
		LIMIT 1`, index, clean, `\\?\`+clean).Scan(
		&e.ID, &e.PartID, &e.ItemID, &e.Index, &url, &e.SubIndex)
	if err != nil {
		return External{}, err
	}
	e.Path = PathFromURL(url)
	return e, nil
}

// --- writing ----------------------------------------------------------------

// NewStream is an audio track about to be added to the library.
type NewStream struct {
	ItemID   int64
	PartID   int64
	Index    int               // >= ExternalIndexBase
	Path     string            // the audio file on disk
	SubIndex int               // which stream inside that file
	Codec    string            // as ffprobe names it: flac, ac3, aac, ...
	Language string            // ISO 639-1, or empty when unknown
	Channels int               // 0 when unknown
	Bitrate  int               // 0 when unknown
	Extra    map[string]string // ma:* keys, see below
}

// Insert adds one audio track.
//
// Three of these columns have shapes that Plex will not tolerate being wrong,
// and none of them fails loudly:
//
//   - extra_data must be valid JSON or NULL. Plex reads it with the ->>
//     operator, and a single malformed row does not break that row - it breaks
//     the whole query, so the track list of every file collapses at once. An
//     empty string counts as malformed; NULL is fine.
//   - language is ISO 639-1, two letters, the way Plex writes it. ffprobe hands
//     out three-letter ISO 639-2 codes, which have to be mapped first.
//   - created_at and updated_at are declared dt_integer and hold unix seconds.
//     Text dates survive right up until something does arithmetic on them.
func (d *DB) Insert(s NewStream) error {
	var extra any
	if len(s.Extra) > 0 {
		b, err := json.Marshal(s.Extra)
		if err != nil {
			return fmt.Errorf("encoding extra_data for %s: %w", s.Path, err)
		}
		extra = string(b)
	}

	// Unknown is NULL, not zero: a literal 0 reads as "this track is 0 kbps"
	// rather than "we do not know", and Plex shows it as such.
	var channels, bitrate any
	if s.Channels > 0 {
		channels = s.Channels
	}
	if s.Bitrate > 0 {
		bitrate = s.Bitrate
	}
	var language any
	if s.Language != "" {
		language = s.Language
	}

	now := time.Now().Unix()
	_, err := d.sql.Exec(`
		INSERT INTO media_streams
			(stream_type_id, media_item_id, media_part_id, "index", url, url_index,
			 codec, language, channels, bitrate, "default", forced,
			 created_at, updated_at, extra_data)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, ?, ?, ?)`,
		streamTypeAudio, s.ItemID, s.PartID, s.Index, FileURL(s.Path), s.SubIndex,
		s.Codec, language, channels, bitrate, now, now, extra)
	if err != nil {
		return fmt.Errorf("adding %s: %w", s.Path, err)
	}
	return nil
}

// Delete removes specific rows.
func (d *DB) Delete(ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tx, err := d.sql.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`DELETE FROM media_streams WHERE id = ?`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	var n int64
	for _, id := range ids {
		res, err := stmt.Exec(id)
		if err != nil {
			return n, fmt.Errorf("removing stream %d: %w", id, err)
		}
		c, _ := res.RowsAffected()
		n += c
	}
	return n, tx.Commit()
}

// DeleteAll removes every row this program ever added.
func (d *DB) DeleteAll() (int64, error) {
	res, err := d.sql.Exec(
		`DELETE FROM media_streams WHERE "index" >= ? AND url IS NOT NULL AND url != ''`,
		ExternalIndexBase)
	if err != nil {
		return 0, fmt.Errorf("removing external streams: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// RepairSelections clears references to audio streams that no longer exist.
//
// Plex does not record the chosen audio track on the stream row. It keeps a
// reference in media_part_settings.selected_audio_stream_id pointing at a
// media_streams.id, and our rows are deleted and recreated often enough - by a
// cleanup, by the audio file disappearing, by Plex wiping them during a library
// re-analysis - that the id on the other end changes and the reference is left
// dangling.
//
// The consequence is out of all proportion to the cause. With a dangling
// reference Plex marks no audio stream as selected at all, and the web player
// takes the selected track straight from that flag with nothing to fall back
// on: it looks up streams.find(s => s.selected), gets undefined, reads .id off
// it, and the whole player dies with "an unexpected error occurred during
// playback" the moment the audio menu is opened. Every file with more than one
// audio track is affected, and it does not recover on its own.
//
// So this runs at the end of every pass, not only after a deletion of ours.
func (d *DB) RepairSelections() (int64, error) {
	res, err := d.sql.Exec(`
		UPDATE media_part_settings
		SET selected_audio_stream_id = NULL
		WHERE selected_audio_stream_id IS NOT NULL
		  AND selected_audio_stream_id NOT IN (SELECT id FROM media_streams)`)
	if err != nil {
		return 0, fmt.Errorf("clearing stale track selections: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
