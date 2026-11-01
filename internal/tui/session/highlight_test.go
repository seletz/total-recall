package session

import (
	"reflect"
	"testing"
)

func TestMatches(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		terms []string
		want  []Match
	}{
		{"no terms", []string{"hello"}, nil, nil},
		{"empty term ignored", []string{"hello"}, []string{""}, nil},
		{"no hit", []string{"hello"}, []string{"world"}, nil},
		{"exact", []string{"run the tests"}, []string{"tests"}, []Match{{0, 8, 13}}},
		{"case-insensitive", []string{"Release v0.1.0"}, []string{"release"}, []Match{{0, 0, 7}}},
		{"term upper-case", []string{"release"}, []string{"RELEASE"}, []Match{{0, 0, 7}}},
		{"umlaut in text", []string{"Über uns"}, []string{"uber"}, []Match{{0, 0, 5}}},
		{"umlaut in term", []string{"Uber uns"}, []string{"über"}, []Match{{0, 0, 4}}},
		{"umlaut both", []string{"schön"}, []string{"SCHÖN"}, []Match{{0, 0, 6}}},
		{"combining mark", []string{"café au lait"}, []string{"café"}, []Match{{0, 0, 6}}},
		{"sharp s is not ss", []string{"Straße"}, []string{"strasse"}, nil},
		{"repeated in line", []string{"go test, go vet"}, []string{"go"}, []Match{{0, 0, 2}, {0, 9, 11}}},
		{"several lines and terms", []string{"build it", "", "vet it", "Build succeeded."}, []string{"vet", "build"},
			[]Match{{0, 0, 5}, {2, 0, 3}, {3, 0, 5}}},
		{"overlap merged", []string{"gopher"}, []string{"go", "gopher"}, []Match{{0, 0, 6}}},
		{"adjacent merged", []string{"foobar"}, []string{"foo", "bar"}, []Match{{0, 0, 6}}},
		{"wide runes", []string{"日本語 go"}, []string{"go"}, []Match{{0, 10, 12}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Matches(tt.lines, tt.terms)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Matches(%q, %q) = %v, want %v", tt.lines, tt.terms, got, tt.want)
			}
		})
	}
}

func TestFold(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"ABC", "abc"},
		{"Äpfel", "apfel"},
		{"ÀÉÎÕÜ", "aeiou"},
		{"ñ ç ø", "n c o"},
		{"Straße", "straße"},
		{"é", "e"},
		{"Łódź", "lodz"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			runes, _ := fold(tt.in)
			if got := string(runes); got != tt.want {
				t.Errorf("fold(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
