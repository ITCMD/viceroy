package email

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func load(t *testing.T, name string) Message {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestParseMessageHTML(t *testing.T) {
	m := load(t, "chase.eml")
	if m.MessageID != "chase-1@alerts.chase.com" || m.FromAddr != "no.reply.alerts@chase.com" || m.FromName != "Chase" {
		t.Fatalf("headers: %+v", m)
	}
	if strings.Contains(m.Text, "<") || strings.Contains(m.Text, "color:red") {
		t.Fatalf("html not converted: %q", m.Text)
	}
	if !strings.Contains(m.Text, "Merchant\nSTARBUCKS STORE 123") || !strings.Contains(m.Text, "© 2026") {
		t.Fatalf("text: %q", m.Text)
	}
}

func TestTemplates(t *testing.T) {
	cases := []struct {
		file, parser string
		want         Parsed
	}{
		{"chase.eml", "chase", Parsed{1234, "STARBUCKS STORE 123", "2026-09-29"}},
		{"chase.eml", "generic", Parsed{1234, "STARBUCKS STORE 123", "2026-09-29"}},
		{"capitalone.eml", "capital_one", Parsed{104520, "TRADER JOE'S #552", "2026-09-28"}},
		{"capitalone.eml", "generic", Parsed{104520, "TRADER JOE'S #552", "2026-09-28"}},
		{"labeled.eml", "generic", Parsed{4810, "SHELL OIL 57442", "2026-09-30"}},
	}
	for _, c := range cases {
		got, err := Parse(c.parser, "", load(t, c.file))
		if err != nil {
			t.Errorf("%s/%s: %v", c.file, c.parser, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s/%s: got %+v, want %+v", c.file, c.parser, got, c.want)
		}
	}
	if _, err := Parse("generic", "", load(t, "unknown.eml")); err == nil {
		t.Error("newsletter parsed; want no amount error")
	}
}

func TestCustomParser(t *testing.T) {
	m := load(t, "labeled.eml")
	spec := func(c CustomParser) string { b, _ := json.Marshal(c); return string(b) }

	got, err := Parse("custom", spec(CustomParser{
		Amount: FieldSpec{Before: "Amount:"}, Merchant: FieldSpec{Before: "Merchant:"}, Date: FieldSpec{Before: "Date:"},
	}), m)
	if err != nil || got != (Parsed{4810, "SHELL OIL 57442", "2026-09-30"}) {
		t.Fatalf("before-only: %+v %v", got, err)
	}
	got, err = Parse("custom", spec(CustomParser{
		Amount: FieldSpec{Regex: `\$([\d.]+)`}, Merchant: FieldSpec{Before: "Merchant:", After: "5744"},
	}), m)
	if err != nil || got.Merchant != "SHELL OIL" || got.AmountCents != 4810 || got.Date != "2026-09-30" {
		t.Fatalf("regex/after: %+v %v", got, err)
	}
	if err := ValidateParser("custom", spec(CustomParser{Amount: FieldSpec{Regex: "("}, Merchant: FieldSpec{Before: "x"}})); err == nil {
		t.Error("bad regex accepted")
	}
	if err := ValidateParser("custom", spec(CustomParser{Amount: FieldSpec{Before: "x"}})); err == nil {
		t.Error("missing merchant accepted")
	}
	if _, err := Parse("custom", spec(CustomParser{Amount: FieldSpec{Before: "Total:"}, Merchant: FieldSpec{Before: "Merchant:"}}), m); err == nil {
		t.Error("missing amount parsed")
	}
}

func TestParseDateFallback(t *testing.T) {
	recv := time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local)
	for in, want := range map[string]string{
		"":                   "2026-09-30",
		"Sept 3, 2026":       "2026-09-03",
		"Tuesday, 9/29/26":   "2026-09-29",
		"January 5, 2020":    "2026-09-30", // implausibly old
		"October 9, 2026":    "2026-09-30", // future
		"sometime yesterday": "2026-09-30",
	} {
		if got := parseDate(in, recv); got != want {
			t.Errorf("parseDate(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestFilterMatches(t *testing.T) {
	chase, cu := load(t, "chase.eml"), load(t, "labeled.eml")
	cases := []struct {
		f    Filter
		m    Message
		want bool
	}{
		{Filter{Sender: "chase.com"}, chase, true},
		{Filter{Sender: "@CHASE.com"}, chase, true},
		{Filter{Sender: "no.reply.alerts@chase.com"}, chase, true},
		{Filter{Sender: "alerts@chase.com"}, chase, false},
		{Filter{Sender: "hase.com"}, chase, false},
		{Filter{Sender: "chase.com", Subject: "your $"}, chase, true},
		{Filter{Sender: "chase.com", Body: "(...1234)"}, chase, true},
		{Filter{Sender: "chase.com", Body: "(...9999)"}, chase, false},
		{Filter{Subject: `purchase\s+alert$`, Regex: true}, cu, true},
		{Filter{Body: `ending in 4321`}, cu, true},
		{Filter{Sender: "mycu.org", Body: "ending in 1111"}, cu, false},
	}
	for i, c := range cases {
		if got := c.f.Matches(c.m); got != c.want {
			t.Errorf("case %d %+v: got %v", i, c.f, got)
		}
	}
	if (Filter{}).Validate() == nil || (Filter{Subject: "(", Regex: true}).Validate() == nil {
		t.Error("invalid filters accepted")
	}
}
