package main

import (
	"regexp"
	"strings"
)

// ContentFilter blocks tunnel registration and traffic serving clearly
// illegal categories (CSAM / child sexual abuse material and closely
// related abuse). The design goal is ZERO false positives on benign
// content: it fires only on (a) unambiguous phrases, or (b) a strict
// co-occurrence of a sexual-explicit term and a minor term within one
// short window. Single benign words ("child", "kids", "school", "teen",
// "video", "girl") never trigger anything.
type ContentFilter struct {
	Enabled bool
	re      *regexp.Regexp
	hits    int
	banned  map[string]bool
}

// cat1 — unambiguous phrases (each alone is grounds for a block).
// Languages: EN / RU / translit / common leetspeak is normalized first.
var cat1Phrases = []string{
	// EN explicit
	"child porn", "childporn", "childporno", "kids porn", "kidsporn",
	"kid porn", "children porn", "childsex", "cporn", "preteen porn",
	"preteen sex", "underage porn", "underage sex", "jailbait porn",
	"loli porn", "shota porn", "lolibooru", "cunny", "child nudism",
	"child nudist", "pedo content", "pedophile content", "pedofile content",
	"child sexual abuse", "child abuse material", "csam material",
	"child fuck", "child rape", "rape child", "molest child",
	"nudify child", "nudify kids", "nudify teen", "undress child",
	"undress kids", "cp site", "dark web cp",
	// RU explicit
	"порно с дет", "порно детей", "порно детей", "детское порно",
	"детей порно", "порно школьник", "школьниц порно", "порно малолет",
	"малолетн порно", "секс с дет", "секс детей", "дети секс",
	"ебать дет", "трах дет", "изнасил дет", "педофил контент",
	"нюды дет", "нюдсы дет", "раздет школьн", "cp раздел",
	// translit
	"porno s det", "porno det", "seks s det", "seks det",
	"detsko porno", "detskoe porno", "ebat det",
}

// cat2 — co-occurrence lists: a sexual-explicit term AND a minor term
// within a small word window (both lists are deliberately narrow).
var cat2Sexual = map[string]bool{
	"porn": true, "pornhub": true, "xxx": true, "sex": true, "sexy": false,
	"nude": true, "nudes": true, "nudify": true, "naked": true,
	"hentai": true, "loli": true, "shota": true, "jailbait": true,
	"camwhore": true, "порно": true, "секс": true, "нюдсы": true,
	"нюды": true, "нюд": true, "хентай": true, "ебля": true,
	"шлюх": false, "раздеть": true, " undress": true,
}

var cat2Minor = map[string]bool{
	"child": true, "children": true, "kid": true, "kids": true, "minor": true,
	"preteen": true, "underage": true, "littlegirl": true, "smallgirl": true,
	"школьник": true, "школьница": true, "школьников": true, "школьниц": true,
	"малолет": true, "малолетк": true, "несовершеннолет": true,
	"дет": true, "ребен": true, "девочк": true, "мальчик": true,
}

var wordSplitRegex = regexp.MustCompile(`[^a-zA-Zа-яА-Я0-9]+`)

// leet maps common obfuscations back to letters before matching.
var leetReplacer = strings.NewReplacer(
	"0", "o", "1", "i", "3", "e", "4", "a", "5", "s", "7", "t",
	"@dulti", " ", "ph", "f", "rn", "m",
)

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// NewContentFilter builds the filter. enabled=false matches nothing.
func NewContentFilter(enabled bool) *ContentFilter {
	parts := make([]string, 0, len(cat1Phrases))
	for _, p := range cat1Phrases {
		parts = append(parts, regexp.QuoteMeta(p))
	}
	re, err := regexp.Compile("(?i)(" + strings.Join(parts, "|") + ")")
	if err != nil {
		re = regexp.MustCompile(`\z.`) // never matches
	}
	return &ContentFilter{Enabled: enabled, re: re, banned: map[string]bool{}}
}

// normalize: lowercase, strip separators, join words with single spaces.
func normalize(s string) string {
	s = strings.ToLower(s)
	words := wordSplitRegex.Split(s, -1)
	out := make([]string, 0, len(words))
	for _, w := range words {
		if w != "" {
			out = append(out, w)
		}
	}
	return strings.Join(out, " ")
}

// deLeet normalizes obfuscated spellings (ch1ld -> child).
func deLeet(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "ch1ld", "child")
	s = strings.ReplaceAll(s, "k1ds", "kids")
	s = strings.ReplaceAll(s, "k1d", "kid")
	s = strings.ReplaceAll(s, "p0rn", "porn")
	s = strings.ReplaceAll(s, "s3x", "sex")
	s = strings.ReplaceAll(s, "n0rn", "porn")
	s = strings.ReplaceAll(s, " pron ", " porn ")
	return s
}

// CheckHost decides whether a host/label may serve traffic.
func (cf *ContentFilter) CheckHost(host string) bool {
	if !cf.Enabled {
		return true
	}
	if cf.banned[host] {
		return false
	}
	n := normalize(deLeet(host))
	if cf.re.MatchString(n) {
		cf.banned[host] = true
		cf.hits++
		return false
	}
	words := strings.Fields(n)
	hasMinor, hasSexual := false, false
	for _, w := range words {
		if cat2Minor[trimSuffixMatch(w, cat2Minor)] || cat2Minor[w] {
			hasMinor = true
		}
		if cat2Sexual[w] {
			hasSexual = true
		}
	}
	if hasMinor && hasSexual {
		cf.banned[host] = true
		cf.hits++
		return false
	}
	return true
}

// CheckBody inspects request metadata (host, path, headers sample, text
// sample) and returns false when the host must be blocked.
func (cf *ContentFilter) CheckBody(host, sample string) bool {
	if !cf.Enabled {
		return true
	}
	if cf.banned[host] {
		return false
	}
	n := normalize(deLeet(sample))
	if cf.re.MatchString(n) {
		cf.banned[host] = true
		cf.hits++
		return false
	}
	// co-occurrence: minor word with a sexual word within 6 words
	words := strings.Fields(n)
	for i, w := range words {
		if !cat2Minor[w] {
			continue
		}
		lo := i - 6
		if lo < 0 {
			lo = 0
		}
		hi := minInt(i+7, len(words))
		for _, w2 := range words[lo:hi] {
			if cat2Sexual[w2] {
				cf.banned[host] = true
				cf.hits++
				return false
			}
		}
	}
	return true
}

// trimSuffixMatch finds a key that w starts with (for Russian morphology:
// "детского" starts with "дет").
func trimSuffixMatch(w string, m map[string]bool) string {
	for k := range m {
		if len(k) >= 4 && strings.HasPrefix(w, k) {
			return k
		}
	}
	return ""
}

// Blocked reports whether host is currently banned.
func (cf *ContentFilter) Blocked(host string) bool { return cf.banned[host] }

// Hits returns total filter trips (for stats).
func (cf *ContentFilter) Hits() int { return cf.hits }
