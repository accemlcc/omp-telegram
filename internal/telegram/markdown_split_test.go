package telegram

import (
	"strings"
	"testing"
)

func TestConvertMarkdownWideTableFallsBackToLabelledLines(t *testing.T) {
	in := "| Tool | Zweck | Beispiel |\n|---|---|---|\n| `read` | Dateien, Archive, SQLite, URLs und mehr | `read(\"x.ts:1-2\")` |\n| `bash` | Echte Binaries | `df -h` |"
	want := "Tool: read · Zweck: Dateien, Archive, SQLite, URLs und mehr · Beispiel: read(\"x.ts:1-2\")\nTool: bash · Zweck: Echte Binaries · Beispiel: df -h"
	got := ConvertMarkdown(in)
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, "<pre>") {
		t.Fatal("wide table must not use a fixed-width block")
	}
}

func TestSplitForTelegramKeepsEveryPartConvertible(t *testing.T) {
	var b strings.Builder
	b.WriteString("Einleitung mit **fett** und `code`.\n\n")
	b.WriteString("| Tool | Zweck | Beispiel |\n|---|---|---|\n")
	for i := 0; i < 40; i++ {
		b.WriteString("| read | Dateien, Archive, SQLite, URLs und lange Erklaerungen | read(\"x.ts:1-2\") |\n")
	}
	b.WriteString("\n```go\n")
	b.WriteString(strings.Repeat("fmt.Println(\"zeile\")\n", 120))
	b.WriteString("```\n\n")
	b.WriteString(strings.Repeat("Prosa mit A & B < C und *betontem* Text. ", 200))
	md := b.String()

	parts := SplitForTelegram(md, MaxMessageUTF16)
	if len(parts) < 2 {
		t.Fatalf("expected the message to split, got %d part(s)", len(parts))
	}
	for i, part := range parts {
		if n := utf16Len(ConvertMarkdown(part)); n > MaxMessageUTF16 {
			t.Fatalf("part %d converts to %d units, over the %d limit", i, n, MaxMessageUTF16)
		}
		if strings.Count(part, "```")%2 != 0 {
			t.Fatalf("part %d has an unbalanced code fence", i)
		}
	}
	if got := strings.Count(strings.Join(parts, "\n\n"), "read | Dateien"); got != 40 {
		t.Fatalf("table rows lost: %d of 40 survived", got)
	}
	if got := strings.Count(strings.Join(parts, "\n"), "fmt.Println"); got != 120 {
		t.Fatalf("code lines lost: %d of 120 survived", got)
	}
}

func TestSplitForTelegramRepeatsTableHeaderAcrossParts(t *testing.T) {
	var b strings.Builder
	b.WriteString("| name | note |\n|---|---|\n")
	for i := 0; i < 30; i++ {
		b.WriteString("| zeile | ")
		b.WriteString(strings.Repeat("x", 60))
		b.WriteString(" |\n")
	}
	parts := SplitForTelegram(b.String(), 400)
	if len(parts) < 2 {
		t.Fatalf("expected a split, got %d part(s)", len(parts))
	}
	for i, part := range parts {
		lines := strings.Split(part, "\n")
		if len(lines) < 3 {
			t.Fatalf("part %d lost its table shape: %q", i, part)
		}
		if lines[0] != "| name | note |" || !tableDelim.MatchString(lines[1]) {
			t.Fatalf("part %d has no repeated header: %q / %q", i, lines[0], lines[1])
		}
	}
}

func TestSplitForTelegramKeepsSmallBlocksTogether(t *testing.T) {
	md := "alpha\n\nbeta `x` gamma"
	parts := SplitForTelegram(md, MaxMessageUTF16)
	if len(parts) != 1 {
		t.Fatalf("parts = %d, want 1", len(parts))
	}
	if ConvertMarkdown(parts[0]) != ConvertMarkdown(md) {
		t.Fatalf("single part must convert identically, got %q", ConvertMarkdown(parts[0]))
	}
	if SplitForTelegram("   \n\n", MaxMessageUTF16) != nil {
		t.Fatal("blank input must yield no parts")
	}
}

func TestClipConvertibleFitsAndMarksTheCut(t *testing.T) {
	long := strings.Repeat("**x** & `y` ", 700) // 700 * 10 = 7000 units
	got := ClipConvertible(long, 1200)
	if n := utf16Len(ConvertMarkdown(got)); n > 1200 {
		t.Fatalf("clipped text converts to %d units, over the 1200 limit", n)
	}
	if !strings.HasSuffix(got, "\n…") {
		t.Fatalf("clipped text must mark the cut, got %q", got[len(got)-20:])
	}
	if short := "kurz `x`"; ClipConvertible(short, MaxMessageUTF16) != short {
		t.Fatal("short text must pass through untouched")
	}
}
