package fitsheader

import (
	"fmt"
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

func TestWriteFloat(t *testing.T) {
	path := writeFITS(t, []string{
		"CCD-TEMP=                 0.05 / sensor temperature",
		"EXPTIME =                 600.0",
	}, true)

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	found, err := WriteFloat(path, "CCD-TEMP", 0)
	if err != nil {
		t.Fatalf("WriteFloat(): %v", err)
	}
	if !found {
		t.Fatal("WriteFloat() found = false")
	}

	got, ok, err := ReadFloat(path, "CCD-TEMP")
	if err != nil {
		t.Fatalf("ReadFloat(): %v", err)
	}
	if !ok || got != 0 {
		t.Fatalf("CCD-TEMP = %v, %v, want 0", got, ok)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatalf("file size changed from %d to %d", len(before), len(after))
	}

	// SIMPLE is card 0, CCD-TEMP card 1.
	beforeCard := before[cardSize : 2*cardSize]
	afterCard := after[cardSize : 2*cardSize]
	if string(beforeCard[30:]) != string(afterCard[30:]) {
		t.Errorf("comment/padding changed: %q -> %q", beforeCard[30:], afterCard[30:])
	}
	if string(afterCard[10:30]) != "                   0" {
		t.Errorf("value field = %q, want right-justified 0", afterCard[10:30])
	}

	// Other cards are untouched.
	exp, ok, err := ReadFloat(path, "EXPTIME")
	if err != nil || !ok || exp != 600 {
		t.Fatalf("EXPTIME = %v, %v, %v, want 600", exp, ok, err)
	}
}

func TestWriteFloatBeyondFirstBlock(t *testing.T) {
	cards := append(repeatCard("HISTORY ", 40), "CCD-TEMP=                 0.05")
	path := writeFITS(t, cards, true)

	found, err := WriteFloat(path, "CCD-TEMP", -15)
	if err != nil || !found {
		t.Fatalf("WriteFloat() = %v, %v", found, err)
	}
	got, ok, err := ReadFloat(path, "CCD-TEMP")
	if err != nil || !ok || got != -15 {
		t.Fatalf("CCD-TEMP = %v, %v, %v, want -15", got, ok, err)
	}
}

func TestWriteFloatMissing(t *testing.T) {
	path := writeFITS(t, []string{"EXPTIME =                 600.0"}, true)
	found, err := WriteFloat(path, "CCD-TEMP", 0)
	if err != nil {
		t.Fatalf("WriteFloat(): %v", err)
	}
	if found {
		t.Fatal("WriteFloat() found = true for a missing card")
	}
}

func TestWriteFloatTooLong(t *testing.T) {
	path := writeFITS(t, []string{"CCD-TEMP=                 0.05"}, true)
	if _, err := WriteFloat(path, "CCD-TEMP", 1e30); err == nil {
		t.Fatal("WriteFloat() = nil error, want a value-field overflow error")
	}
	got, ok, err := ReadFloat(path, "CCD-TEMP")
	if err != nil || !ok || got != 0.05 {
		t.Fatalf("CCD-TEMP = %v, %v, %v, want unchanged 0.05", got, ok, err)
	}
}

func TestReadString(t *testing.T) {
	tests := []struct {
		name    string
		cards   []string
		want    string
		wantOk  bool
		wantErr bool
	}{
		{
			name:   "value with padding",
			cards:  []string{fmt.Sprintf("IMAGETYP= %-20s / image type", "'Dark Frame'")},
			want:   "Dark Frame",
			wantOk: true,
		},
		{
			name:   "doubled quote",
			cards:  []string{"IMAGETYP= 'O''Brien'"},
			want:   "O'Brien",
			wantOk: true,
		},
		{
			name:   "missing",
			cards:  []string{"EXPTIME =                  600.0"},
			wantOk: false,
		},
		{
			name:    "numeric value",
			cards:   []string{"IMAGETYP=                   100"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFITS(t, tt.cards, true)
			got, ok, err := ReadString(path, "IMAGETYP")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ReadString() = %q, %v, want error", got, ok)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadString(): %v", err)
			}
			if ok != tt.wantOk {
				t.Fatalf("found = %v, want %v", ok, tt.wantOk)
			}
			if ok && got != tt.want {
				t.Fatalf("ReadString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteString(t *testing.T) {
	card := fmt.Sprintf("IMAGETYP= %-20s / image type", "'Bias Frame'")
	path := writeFITS(t, []string{card, "EXPTIME =                 600.0"}, true)

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	found, err := WriteString(path, "IMAGETYP", "Dark Frame")
	if err != nil {
		t.Fatalf("WriteString(): %v", err)
	}
	if !found {
		t.Fatal("WriteString() found = false")
	}

	got, ok, err := ReadString(path, "IMAGETYP")
	if err != nil || !ok || got != "Dark Frame" {
		t.Fatalf("IMAGETYP = %q, %v, %v, want Dark Frame", got, ok, err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatalf("file size changed from %d to %d", len(before), len(after))
	}

	beforeCard := before[cardSize : 2*cardSize]
	afterCard := after[cardSize : 2*cardSize]
	if string(beforeCard[30:]) != string(afterCard[30:]) {
		t.Errorf("comment/padding changed: %q -> %q", beforeCard[30:], afterCard[30:])
	}
	if string(afterCard[10:30]) != "'Dark Frame'        " {
		t.Errorf("value field = %q, want left-justified string", afterCard[10:30])
	}
}

func TestWriteStringEmbeddedQuote(t *testing.T) {
	path := writeFITS(t, []string{"IMAGETYP= 'Bias Frame'"}, true)
	if _, err := WriteString(path, "IMAGETYP", "O'Brien"); err != nil {
		t.Fatalf("WriteString(): %v", err)
	}
	got, ok, err := ReadString(path, "IMAGETYP")
	if err != nil || !ok || got != "O'Brien" {
		t.Fatalf("IMAGETYP = %q, %v, %v, want O'Brien", got, ok, err)
	}
}

func TestWriteStringMissing(t *testing.T) {
	path := writeFITS(t, []string{"EXPTIME =                 600.0"}, true)
	found, err := WriteString(path, "IMAGETYP", "Dark Frame")
	if err != nil {
		t.Fatalf("WriteString(): %v", err)
	}
	if found {
		t.Fatal("WriteString() found = true for a missing card")
	}
}

func TestWriteStringTooLong(t *testing.T) {
	path := writeFITS(t, []string{"IMAGETYP= 'Bias Frame'"}, true)
	if _, err := WriteString(path, "IMAGETYP", strings.Repeat("x", 19)); err == nil {
		t.Fatal("WriteString() = nil error, want a value-field overflow error")
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
