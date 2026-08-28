package plex

import "strings"

// Language normalises a language code into the form Plex stores.
//
// Containers carry ISO 639-2 three-letter codes ("rus", "jpn"); Plex's
// media_streams.language column holds ISO 639-1 two-letter ones ("ru", "ja").
// On the library this was developed against, 20469 of the 20633 rows Plex had
// written itself were two letters.
//
// Anything that cannot be mapped comes back empty, and that is on purpose.
// "und" is not a code Plex writes, and inventing one is worse than admitting we
// do not know: an empty language simply shows the track by its title, which for
// an alternative dub is the more useful label anyway.
//
// Codes that are already two letters pass through, as do region tags like
// "pt-BR" which Plex stores verbatim.
func Language(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" || code == "und" || code == "unknown" || code == "mis" || code == "zxx" {
		return ""
	}
	if len(code) == 2 || strings.ContainsAny(code, "-_") {
		return strings.ReplaceAll(code, "_", "-")
	}
	if two, ok := iso6392[code]; ok {
		return two
	}
	return ""
}

// Both the bibliographic (/B) and terminological (/T) spellings appear in real
// files, so both are listed - "ger" and "deu" are the same language, and which
// one a muxer wrote down is not something we get to choose.
var iso6392 = map[string]string{
	"alb": "sq", "sqi": "sq",
	"ara": "ar",
	"arm": "hy", "hye": "hy",
	"aze": "az",
	"baq": "eu", "eus": "eu",
	"bel": "be", "ben": "bn", "bos": "bs", "bul": "bg",
	"bur": "my", "mya": "my",
	"cat": "ca",
	"chi": "zh", "zho": "zh",
	"cze": "cs", "ces": "cs",
	"dan": "da",
	"dut": "nl", "nld": "nl",
	"eng": "en", "epo": "eo", "est": "et",
	"fao": "fo",
	"per": "fa", "fas": "fa",
	"fin": "fi",
	"fre": "fr", "fra": "fr",
	"geo": "ka", "kat": "ka",
	"ger": "de", "deu": "de",
	"gle": "ga",
	"gre": "el", "ell": "el",
	"heb": "he", "hin": "hi", "hrv": "hr", "hun": "hu",
	"ice": "is", "isl": "is",
	"ind": "id", "ita": "it", "jav": "jv",
	"jpn": "ja", "jap": "ja",
	"kan": "kn", "kaz": "kk", "khm": "km", "kor": "ko",
	"lat": "la", "lav": "lv", "lit": "lt",
	"mac": "mk", "mkd": "mk",
	"mal": "ml", "mar": "mr",
	"may": "ms", "msa": "ms",
	"mon": "mn", "nor": "no", "nob": "nb", "nno": "nn",
	"pol": "pl", "por": "pt",
	"rum": "ro", "ron": "ro",
	"rus": "ru",
	"slo": "sk", "slk": "sk",
	"slv": "sl", "spa": "es", "srp": "sr", "swe": "sv",
	"tam": "ta", "tel": "te", "tha": "th",
	"tib": "bo", "bod": "bo",
	"tur": "tr", "ukr": "uk", "urd": "ur", "vie": "vi",
	"wel": "cy", "cym": "cy",
}
