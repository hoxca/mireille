// Package fitsheader reads cards from the primary header of a FITS file
// without decoding its pixel data.
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

	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false, fmt.Errorf("%s: %q is not a number", keyword, raw)
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

	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false, fmt.Errorf("%s: %q is not an integer", keyword, raw)
	}
	return v, true, nil
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

		raw, err := cardValue(card[:])
		if err != nil {
			return "", false, fmt.Errorf("%s: %w", keyword, err)
		}
		return raw, true, nil
	}
}

// WriteFloat rewrites the value of the first card named keyword in the primary
// header of the FITS file at path. Only that 80-byte card is overwritten; the
// keyword, the comment and any pixel data are left untouched. found is false
// when the header has no such card.
func WriteFloat(path, keyword string, value float64) (found bool, err error) {
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

		updated, err := rewriteCard(card[:], value)
		if err != nil {
			return true, fmt.Errorf("%s: %w", keyword, err)
		}
		if _, err := f.WriteAt(updated, offset); err != nil {
			return true, err
		}
		return true, nil
	}
}

// rewriteCard returns card with its value field replaced by value, keeping the
// keyword, comment and padding in place. The value is right-justified in the
// 20-byte FITS value field.
func rewriteCard(card []byte, value float64) ([]byte, error) {
	if card[8] != '=' {
		return nil, fmt.Errorf("card has no value")
	}

	s := strconv.FormatFloat(value, 'f', -1, 64)
	if len(s) > 20 {
		return nil, fmt.Errorf("%q does not fit the value field", s)
	}

	out := make([]byte, cardSize)
	copy(out, card)

	for i := 10; i < 30; i++ {
		out[i] = ' '
	}
	copy(out[30-len(s):30], s)

	return out, nil
}

// cardValue extracts the value field of a card with the "= " indicator,
// dropping the trailing comment and normalising FITS D exponents.
func cardValue(card []byte) (string, error) {
	if card[8] != '=' {
		return "", fmt.Errorf("card has no value")
	}

	raw := string(card[10:])
	if i := strings.IndexByte(raw, '/'); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.TrimSpace(raw)

	if raw == "" {
		return "", fmt.Errorf("card has an empty value")
	}
	if raw[0] == '\'' {
		return "", fmt.Errorf("value is a string, not a number")
	}

	return strings.NewReplacer("D", "E", "d", "e").Replace(raw), nil
}
