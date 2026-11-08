// Package fitsheader reads and rewrites cards in the primary header of a FITS
// file without decoding its pixel data.
package fitsheader

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// cardSize is the length of one FITS header card in bytes.
const cardSize = 80

// ReadFloat returns the value of the first numeric card named keyword in the
// primary header of the FITS file at path. found is false when the header has
// no such card.
func ReadFloat(path, keyword string) (value float64, found bool, err error) {
	raw, found, err := readValue(path, keyword)
	if err != nil || !found {
		return 0, found, err
	}

	s, err := numericValue(keyword, raw)
	if err != nil {
		return 0, false, err
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false, fmt.Errorf("%s: %q is not a number", keyword, s)
	}
	return v, true, nil
}

// ReadInt returns the value of the first integer card named keyword in the
// primary header of the FITS file at path. found is false when the header has
// no such card.
func ReadInt(path, keyword string) (value int, found bool, err error) {
	raw, found, err := readValue(path, keyword)
	if err != nil || !found {
		return 0, found, err
	}

	s, err := numericValue(keyword, raw)
	if err != nil {
		return 0, false, err
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, false, fmt.Errorf("%s: %q is not an integer", keyword, s)
	}
	return v, true, nil
}

// ReadString returns the value of the first string card named keyword in the
// primary header of the FITS file at path. found is false when the header has
// no such card.
func ReadString(path, keyword string) (value string, found bool, err error) {
	raw, found, err := readValue(path, keyword)
	if err != nil || !found {
		return "", found, err
	}

	v, err := stringValue(keyword, raw)
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// WriteFloat rewrites the value of the first card named keyword in the primary
// header of the FITS file at path. Only that 80-byte card is overwritten; the
// keyword, the comment and any pixel data are left untouched. found is false
// when the header has no such card.
func WriteFloat(path, keyword string, value float64) (found bool, err error) {
	s := strconv.FormatFloat(value, 'f', -1, 64)
	return writeCard(path, keyword, func(card []byte) ([]byte, error) {
		return replaceValueField(card, s, true)
	})
}

// WriteString rewrites the string value of the first card named keyword in the
// primary header of the FITS file at path. The value is quoted and
// left-justified in the FITS value field; the comment is preserved. found is
// false when the header has no such card.
func WriteString(path, keyword, value string) (found bool, err error) {
	s := "'" + strings.ReplaceAll(value, "'", "''") + "'"
	return writeCard(path, keyword, func(card []byte) ([]byte, error) {
		return replaceValueField(card, s, false)
	})
}

// readValue returns the raw value field of the first card named keyword.
func readValue(path, keyword string) (string, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = f.Close() }()

	return readCard(bufio.NewReader(f), keyword)
}

func readCard(r io.Reader, keyword string) (string, bool, error) {
	var card [cardSize]byte
	for {
		if _, err := io.ReadFull(r, card[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return "", false, fmt.Errorf("header ends before the END card")
			}
			return "", false, err
		}

		name := strings.TrimSpace(string(card[:8]))
		if name == "END" {
			return "", false, nil
		}
		if !strings.EqualFold(name, keyword) {
			continue
		}
		if card[8] != '=' {
			return "", false, fmt.Errorf("%s: card has no value", keyword)
		}
		return string(card[10:]), true, nil
	}
}

// writeCard rewrites the first card named keyword using rewrite, which receives
// the original 80-byte card and returns its replacement.
func writeCard(path, keyword string, rewrite func([]byte) ([]byte, error)) (found bool, err error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()

	var card [cardSize]byte
	var offset int64
	for {
		if _, err := io.ReadFull(f, card[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return false, fmt.Errorf("header ends before the END card")
			}
			return false, err
		}

		name := strings.TrimSpace(string(card[:8]))
		if name == "END" {
			return false, nil
		}
		if !strings.EqualFold(name, keyword) {
			offset += cardSize
			continue
		}

		updated, err := rewrite(card[:])
		if err != nil {
			return true, fmt.Errorf("%s: %w", keyword, err)
		}
		if _, err := f.WriteAt(updated, offset); err != nil {
			return true, err
		}
		return true, nil
	}
}

// replaceValueField returns card with its value field replaced by s, keeping
// the keyword, comment and padding in place.
func replaceValueField(card []byte, s string, rightJustify bool) ([]byte, error) {
	if card[8] != '=' {
		return nil, fmt.Errorf("card has no value")
	}
	if len(s) > 20 {
		return nil, fmt.Errorf("%q does not fit the value field", s)
	}

	out := make([]byte, cardSize)
	copy(out, card)

	for i := 10; i < 30; i++ {
		out[i] = ' '
	}
	if rightJustify {
		copy(out[30-len(s):30], s)
	} else {
		copy(out[10:10+len(s)], s)
	}

	return out, nil
}

// numericValue extracts the numeric value from a raw value field, dropping the
// trailing comment and normalising FITS D exponents.
func numericValue(keyword, raw string) (string, error) {
	if i := strings.IndexByte(raw, '/'); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.TrimSpace(raw)

	if raw == "" {
		return "", fmt.Errorf("%s: card has an empty value", keyword)
	}
	if raw[0] == '\'' {
		return "", fmt.Errorf("%s: value is a string, not a number", keyword)
	}

	return strings.NewReplacer("D", "E", "d", "e").Replace(raw), nil
}

// stringValue extracts the quoted string from a raw value field, un-doubling
// embedded quotes and trimming trailing blanks.
func stringValue(keyword, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%s: card has an empty value", keyword)
	}
	if raw[0] != '\'' {
		return "", fmt.Errorf("%s: value is not a string", keyword)
	}

	var b strings.Builder
	closed := false
	for i := 1; i < len(raw); i++ {
		if raw[i] != '\'' {
			b.WriteByte(raw[i])
			continue
		}
		if i+1 < len(raw) && raw[i+1] == '\'' {
			b.WriteByte('\'')
			i++
			continue
		}
		closed = true
		break
	}
	if !closed {
		return "", fmt.Errorf("%s: unterminated string", keyword)
	}

	return strings.TrimRight(b.String(), " "), nil
}
