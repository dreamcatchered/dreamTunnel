package main

import (
	"regexp"
	"strings"
)

// ContentFilter blocks traffic to tunnel hosts serving clearly illegal
// categories (CSAM / child sexual abuse material). It is deliberately
// narrow: fires only on explicit unambiguous phrases or a strict
// co-occurrence of a sexual term and a minor term within one short
// window. Single benign words ("child", "kids", "school") never trigger.
type ContentFilter struct {
	Enabled bool
	re      *regexp.Regexp
	hits    int
	banned  map[string]bool
}

// cat1 — unambiguous phrases (regexp-quoted, joined).
var cat1Phrases = []string{
	"child porn", "childporn", "childporno", "kids porn", "kidsporn",
	"kid porn", "children porn", "childsex", "childsex",
	"child sexual abuse", "child abuse material", "csam material",
	"pedophile content", "pedo content", "порно с дет", "порно детей",
	"секс с дет", "секс детей", "детское порно", "порно школьников",
	"малолетнее порно", "nudify child", "nudify kids", "undress child",
}

// cat2 — co-occurrence lists: sexual term AND minor term in the same window.
var cat2Sexual = map[string]bool{
	"porn": true, "xxx": true, "sex": true, "nude": true, "nudes": true,
	"nudify": true, "naked": true, "hentai": true, "порно": true,
	"секс": true, "нюдсы": true, "нюды": true,
}

var cat2Minor = map[string]bool{
	"child": true, "children": true, "kid": true, "kids": true, "minor": true,
	"teen": true, "школьник": true, "школьница": true, "школьников": true,
	"малолет": true, "малолетки": true, "несовершеннолет": true,
}

var wordSplitRegex = regexp.MustCompile(`[^a-zA-Zа-яА-Я0-9]+`)

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

// normalize lowercases and splits/joins words with single spaces.
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

// CheckHost decides whether a host may serve traffic. Checks both cat1
// phrases and a co-occurrence of label parts (e.g. "childporn" or
// "kidsporn" as a host label).
func (cf *ContentFilter) CheckHost(host string) bool {
	if !cf.Enabled {
		return true
	}
	n := normalize(host)
	if cf.re.MatchString(n) {
		cf.banned[host] = true
		cf.hits++
		return false
	}
	words := strings.Fields(n)
	hasMinor, hasSexual := false, false
	for _, w := range words {
		if cat2Minor[w] {
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

// CheckBody inspects proxied request metadata (host + text sample) and
// returns false when the host must be blocked.
func (cf *ContentFilter) CheckBody(host, sample string) bool {
	if !cf.Enabled {
		return true
	}
	if cf.banned[host] {
		return false
	}
	n := normalize(sample)
	if cf.re.MatchString(n) {
		cf.banned[host] = true
		cf.hits++
		return false
	}
	// co-occurrence: minor word with a sexual word within 6 words
	words := strings.Fields(n)
	for i, w := range words {
		if cat2Minor[w] {
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
	}
	return true
}

// Blocked reports whether host is currently banned.
func (cf *ContentFilter) Blocked(host string) bool { return cf.banned[host] }

// Hits returns total filter trips (for stats).
func (cf *ContentFilter) Hits() int { return cf.hits }
