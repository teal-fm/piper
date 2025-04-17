package musicbrainz

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
)

var symbols = "1234567890!@#$%^&*()-=_+[]{};\"|;'\\<>?/.,~`"

var guffParenWords = []string{
	"a cappella", "acoustic", "bonus", "censored", "clean", "club", "clubmix", "composition",
	"cut", "dance", "demo", "dialogue", "dirty", "edit", "excerpt", "explicit", "extended",
	"instrumental", "interlude", "intro", "karaoke", "live", "long", "main", "maxi", "megamix",
	"mix", "mono", "official", "orchestral", "original", "outro", "outtake", "outtakes", "piano",
	"quadraphonic", "radio", "rap", "re-edit", "reedit", "refix", "rehearsal", "reinterpreted",
	"released", "release", "remake", "remastered", "remaster", "master", "remix", "remixed",
	"remode", "reprise", "rework", "reworked", "rmx", "session", "short", "single", "skit",
	"stereo", "studio", "take", "takes", "tape", "track", "tryout", "uncensored", "unknown",
	"unplugged", "untitled", "version", "ver", "video", "vocal", "vs", "with", "without",
}

type MetadataCleaner struct {
	recordingExpressions []*regexp2.Regexp
	artistExpressions    []*regexp2.Regexp
	parenGuffExpr        *regexp2.Regexp
	preferredScript      string
}

func NewMetadataCleaner(preferredScript string) *MetadataCleaner {
	recordingPatterns := []string{
		`(?<title>.+?)\s+(?<enclosed>\(.+\)|\[.+\]|\{.+\}|\<.+\>)$`,
		`(?<title>.+?)\s+?(?<feat>[\[\(]?(?:feat(?:uring)?|ft)\b\.?)\s*?(?<artists>.+)\s*`,
		`(?<title>.+?)(?:\s+?[\u2010\u2012\u2013\u2014~/-])(?![^(]*\))(?<dash>.*)`,
	}

	artistPatterns := []string{
		`(?<title>.+?)(?:\s*?,)(?<comma>.*)`,
		`(?<title>.+?)(?:\s+?(&|with))(?<dash>.*)`,
	}

	compiledRecording := make([]*regexp2.Regexp, 0, len(recordingPatterns))
	for _, pattern := range recordingPatterns {
		compiledRecording = append(compiledRecording, regexp2.MustCompile(`(?i)`+pattern, 0))
	}

	compiledArtist := make([]*regexp2.Regexp, 0, len(artistPatterns))
	for _, pattern := range artistPatterns {
		compiledArtist = append(compiledArtist, regexp2.MustCompile(`(?i)`+pattern, 0))
	}

	return &MetadataCleaner{
		recordingExpressions: compiledRecording,
		artistExpressions:    compiledArtist,
		preferredScript:      preferredScript,
		parenGuffExpr:        regexp2.MustCompile(`(20[0-9]{2}|19[0-9]{2})`, 0),
	}
}

func (mc *MetadataCleaner) DropForeignChars(text string) string {
	var b strings.Builder
	b.Grow(len(text))

	hasForeign := false
	hasLetter := false

	for _, r := range text {
		if unicode.Is(unicode.Common, r) || mc.isPreferredScript(r) {
			b.WriteRune(r)
			if unicode.IsLetter(r) {
				hasLetter = true
			}
		} else {
			hasForeign = true
		}
	}

	cleaned := strings.TrimSpace(b.String())
	if hasForeign && len(cleaned) > 0 && hasLetter {
		return cleaned
	}
	return text
}

func (mc *MetadataCleaner) isPreferredScript(r rune) bool {
	switch mc.preferredScript {
	case "Latin":
		return unicode.Is(unicode.Latin, r)
	case "Han":
		return unicode.Is(unicode.Han, r)
	case "Cyrillic":
		return unicode.Is(unicode.Cyrillic, r)
	case "Devanagari":
		return unicode.Is(unicode.Devanagari, r)
	default:
		return false
	}
}

func (mc *MetadataCleaner) IsParenTextLikelyGuff(parenText string) bool {
	pt := strings.ToLower(parenText)
	beforeLen := utf8.RuneCountInString(pt)

	for _, guff := range guffParenWords {
		pt = strings.ReplaceAll(pt, guff, "")
	}

	pt, _ = mc.parenGuffExpr.Replace(pt, "", -1, -1)
	afterLen := utf8.RuneCountInString(pt)
	replaced := beforeLen - afterLen

	chars := 0
	guffChars := replaced
	for _, ch := range pt {
		if strings.ContainsRune(symbols, ch) {
			guffChars++
		}
		if unicode.IsLetter(ch) {
			chars++
		}
	}

	return guffChars > chars
}

func (mc *MetadataCleaner) ParenChecker(text string) bool {
	brackets := []struct {
		open, close rune
	}{
		{'(', ')'}, {'[', ']'}, {'{', '}'}, {'<', '>'},
	}
	for _, pair := range brackets {
		if strings.Count(text, string(pair.open)) != strings.Count(text, string(pair.close)) {
			return false
		}
	}
	return true
}

func (mc *MetadataCleaner) CleanRecording(text string) (string, bool) {
	text = strings.TrimSpace(text)

	if !mc.ParenChecker(text) {
		return text, false
	}

	text = mc.DropForeignChars(text)
	var changed bool

	for _, expr := range mc.recordingExpressions {
		match, _ := expr.FindStringMatch(text)
		if match != nil {
			groups := make(map[string]string)
			for _, name := range expr.GetGroupNames() {
				groups[name] = strings.TrimSpace(match.GroupByName(name).String())
			}

			if guffy := groups["enclosed"]; guffy != "" && mc.IsParenTextLikelyGuff(guffy) {
				text = groups["title"]
				changed = true
				break
			}

			if feat := groups["feat"]; feat != "" {
				text = groups["title"]
				changed = true
				break
			}

			if dash := groups["dash"]; dash != "" {
				if mc.IsParenTextLikelyGuff(dash) {
					text = groups["title"]
					changed = true
					break
				}
			}
		}
	}

	return strings.TrimSpace(text), changed
}

func (mc *MetadataCleaner) CleanArtist(text string) (string, bool) {
	text = strings.TrimSpace(text)

	if !mc.ParenChecker(text) {
		return text, false
	}

	text = mc.DropForeignChars(text)
	var changed bool

	for _, expr := range mc.artistExpressions {
		match, _ := expr.FindStringMatch(text)
		if match != nil {
			groups := make(map[string]string)
			for _, name := range expr.GetGroupNames() {
				groups[name] = strings.TrimSpace(match.GroupByName(name).String())
			}

			title := groups["title"]
			if len(title) > 2 && unicode.IsLetter(rune(title[0])) {
				text = title
				changed = true
				break
			}
		}
	}

	return strings.TrimSpace(text), changed
}
