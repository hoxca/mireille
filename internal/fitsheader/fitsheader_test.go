package fitsheader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFITS writes a primary header made of the given cards. The cards are
// padded with blank cards, and the header with spaces up to a 2880-byte block.
func writeFITS(t *testing.T, cards []string, end bool) string {
	t.Helper()

	var b strings.Builder
	line := func(s string) {
		if len(s) > cardSize {
			t.Fatalf("card %q is longer than %d bytes", s, cardSize)
		}
		b.WriteString(s)
		b.WriteString(strings.Repeat(" ", cardSize-len(s)))
	}

	line("SIMPLE  =                    T")
	for _, card := range cards {
		line(card)
	}
	if end {
		line("END")
	}
	for b.Len()%2880 != 0 {
		b.WriteByte(' ')
	}

	path := filepath.Join(t.TempDir(), "frame.fits")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write FITS: %v", err)
	}
	return path
}

func TestReadFloat(t *testing.T) {
	tests := []struct {
		name    string
		cards   []string
		want    float64
		wantOk  bool
		wantErr bool
	}{
		{
			name:   "first block",
			cards:  []string{"CCD-TEMP=                -15.0 / sensor temperature"},
			want:   -15,
			wantOk: true,
		},
		{
			name:   "other keyword does not match",
			cards:  []string{"OBJECT  = 'M31'", "CCD-TEMP=                 12.5"},
			want:   12.5,
			wantOk: true,
		},
		{
			name:   "D exponent",
			cards:  []string{"CCD-TEMP=                 1.5D+01"},
			want:   15,
			wantOk: true,
		},
		{
			name:   "missing",
			cards:  []string{"EXPTIME =                  600.0"},
			wantOk: false,
		},
		{
			name:    "string value",
			cards:   []string{"CCD-TEMP= 'cold'"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFITS(t, tt.cards, true)
			got, ok, err := ReadFloat(path, "CCD-TEMP")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ReadFloat() = %v, %v, want error", got, ok)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadFloat(): %v", err)
			}
			if ok != tt.wantOk {
				t.Fatalf("found = %v, want %v", ok, tt.wantOk)
			}
			if ok && got != tt.want {
				t.Fatalf("ReadFloat() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadFloatBeyondFirstBlock(t *testing.T) {
	cards := make([]string, 40)
	for i := range cards {
		cards[i] = "HISTORY " + strings.Repeat("x", 70)
	}
	cards = append(cards, "CCD-TEMP=                -20.0")

	path := writeFITS(t, cards, true)
	got, ok, err := ReadFloat(path, "CCD-TEMP")
	if err != nil {
		t.Fatalf("ReadFloat(): %v", err)
	}
	if !ok {
		t.Fatal("CCD-TEMP not found in second header block")
	}
	if got != -20 {
		t.Fatalf("ReadFloat() = %v, want -20", got)
	}
}

func TestReadFloatTruncated(t *testing.T) {
	path := writeFITS(t, []string{"EXPTIME =                600.0"}, false)
	if _, _, err := ReadFloat(path, "CCD-TEMP"); err == nil {
		t.Fatal("ReadFloat() = nil error, want a truncated-header error")
	}
}

func TestReadFloatMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.fits")
	if _, _, err := ReadFloat(path, "CCD-TEMP"); err == nil {
		t.Fatal("ReadFloat() = nil error, want an open error")
	}
}

func TestReadInt(t *testing.T) {
	tests := []struct {
		name    string
		keyword string
		cards   []string
		want    int
		wantOk  bool
		wantErr bool
	}{
		{
			name:    "integer",
			keyword: "GAIN",
			cards:   []string{"GAIN    =                  100 / e-/ADU"},
			want:    100,
			wantOk:  true,
		},
		{
			name:    "zero",
			keyword: "OFFSET",
			cards:   []string{"OFFSET  =                    0"},
			wantOk:  true,
		},
		{
			name:    "card beyond first block",
			keyword: "OFFSET",
			cards:   append(repeatCard("HISTORY ", 40), "OFFSET  =                   30"),
			want:    30,
			wantOk:  true,
		},
		{
			name:    "missing",
			keyword: "GAIN",
			cards:   []string{"EXPTIME =                  600.0"},
			wantOk:  false,
		},
		{
			name:    "decimal value",
			keyword: "GAIN",
			cards:   []string{"GAIN    =                  100.5"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFITS(t, tt.cards, true)
			got, ok, err := ReadInt(path, tt.keyword)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ReadInt() = %v, %v, want error", got, ok)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadInt(): %v", err)
			}
			if ok != tt.wantOk {
				t.Fatalf("found = %v, want %v", ok, tt.wantOk)
			}
			if ok && got != tt.want {
				t.Fatalf("ReadInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

// repeatCard returns n HISTORY cards, keeping the header under test away from
// the first 2880-byte block.
func repeatCard(prefix string, n int) []string {
	cards := make([]string, n)
	for i := range cards {
		cards[i] = prefix + strings.Repeat("x", 70)
	}
	return cards
}
