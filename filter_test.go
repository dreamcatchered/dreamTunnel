package main

import "testing"

func TestFilterNarrow(t *testing.T) {
	f := NewContentFilter(true)

	// must block: unambiguous CSAM phrases
	for _, bad := range []string{"child porn", "kids porn video", "порно с детьми", "nudify child"} {
		if f.CheckBody("x.com", bad) {
			t.Errorf("should block: %q", bad)
		}
	}

	// must NOT block: benign single words
	for _, ok := range []string{"child", "kids", "school", "video", "children school",
		"школьник", "видео для детей", "kids school video"} {
		if !f.CheckBody("y.com", ok) {
			t.Errorf("false positive on: %q", ok)
		}
	}
	// host checks
	if f.CheckHost("childporn.maiuni.online") {
		t.Error("should block host")
	}
	if !f.CheckHost("school.maiuni.online") {
		t.Error("should allow host")
	}
	// disabled filter blocks nothing
	f2 := NewContentFilter(false)
	if !f2.CheckBody("z.com", "child porn") {
		t.Error("disabled filter must allow")
	}
}
