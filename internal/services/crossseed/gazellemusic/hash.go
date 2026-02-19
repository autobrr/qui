// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package gazellemusic

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"crypto/sha1" //nolint:gosec // BitTorrent v1 infohash requires SHA1.
)

// CalculateHashesWithSources returns v1 info hashes calculated with different "source" flags.
// This matches gzlx behavior and is used for hash-based matching against Gazelle trackers.
func CalculateHashesWithSources(torrentData []byte, sources []string) (map[string]string, error) {
	decoded, err := decodeBencode(torrentData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode torrent: %w", err)
	}

	torrentDict, ok := decoded.(map[string]any)
	if !ok {
		return nil, errors.New("torrent is not a dictionary")
	}

	infoRaw, ok := torrentDict["info"]
	if !ok {
		return nil, errors.New("torrent has no info dictionary")
	}

	info, ok := infoRaw.(map[string]any)
	if !ok {
		return nil, errors.New("info is not a dictionary")
	}

	result := make(map[string]string)

	// Base hash (no source).
	delete(info, "source")
	encoded, err := encodeBencode(info)
	if err != nil {
		return nil, err
	}
	hash := sha1.Sum(encoded) //nolint:gosec // BitTorrent v1 infohash requires SHA1.
	result[""] = hex.EncodeToString(hash[:])

	for _, source := range sources {
		info["source"] = source
		encoded, err := encodeBencode(info)
		if err != nil {
			return nil, err
		}
		hash := sha1.Sum(encoded) //nolint:gosec // BitTorrent v1 infohash requires SHA1.
		result[source] = hex.EncodeToString(hash[:])
	}

	return result, nil
}

func decodeBencode(data []byte) (any, error) {
	result, _, err := decodeBencodeValue(data, 0)
	return result, err
}

func decodeBencodeValue(data []byte, pos int) (any, int, error) {
	if pos >= len(data) {
		return nil, pos, errors.New("unexpected end of data")
	}
	switch {
	case data[pos] == 'i':
		return decodeBencodeInt(data, pos)
	case data[pos] == 'l':
		return decodeBencodeList(data, pos)
	case data[pos] == 'd':
		return decodeBencodeDict(data, pos)
	case data[pos] >= '0' && data[pos] <= '9':
		return decodeBencodeString(data, pos)
	default:
		return nil, pos, fmt.Errorf("invalid bencode at position %d: %c", pos, data[pos])
	}
}

func decodeBencodeInt(data []byte, pos int) (int64, int, error) {
	pos++ // skip 'i'
	end := bytes.IndexByte(data[pos:], 'e')
	if end == -1 {
		return 0, pos, errors.New("unterminated integer")
	}
	end += pos
	n, err := strconv.ParseInt(string(data[pos:end]), 10, 64)
	if err != nil {
		return 0, pos, err
	}
	return n, end + 1, nil
}

func decodeBencodeString(data []byte, pos int) (string, int, error) {
	colonPos := bytes.IndexByte(data[pos:], ':')
	if colonPos == -1 {
		return "", pos, errors.New("invalid string: no colon")
	}
	colonPos += pos
	length, err := strconv.Atoi(string(data[pos:colonPos]))
	if err != nil {
		return "", pos, err
	}
	start := colonPos + 1
	end := start + length
	if end > len(data) {
		return "", pos, errors.New("string length exceeds data")
	}
	return string(data[start:end]), end, nil
}

func decodeBencodeList(data []byte, pos int) ([]any, int, error) {
	pos++ // skip 'l'
	var result []any
	for pos < len(data) && data[pos] != 'e' {
		val, newPos, err := decodeBencodeValue(data, pos)
		if err != nil {
			return nil, pos, err
		}
		result = append(result, val)
		pos = newPos
	}
	if pos >= len(data) {
		return nil, pos, errors.New("unterminated list")
	}
	return result, pos + 1, nil
}

func decodeBencodeDict(data []byte, pos int) (map[string]any, int, error) {
	pos++ // skip 'd'
	result := make(map[string]any)
	for pos < len(data) && data[pos] != 'e' {
		key, newPos, err := decodeBencodeString(data, pos)
		if err != nil {
			return nil, pos, fmt.Errorf("invalid dict key: %w", err)
		}
		pos = newPos
		val, newPos, err := decodeBencodeValue(data, pos)
		if err != nil {
			return nil, pos, fmt.Errorf("invalid dict value for key %s: %w", key, err)
		}
		result[key] = val
		pos = newPos
	}
	if pos >= len(data) {
		return nil, pos, errors.New("unterminated dict")
	}
	return result, pos + 1, nil
}

func encodeBencode(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := encodeBencodeValue(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeBencodeValue(buf *bytes.Buffer, v any) error {
	switch val := v.(type) {
	case int64:
		fmt.Fprintf(buf, "i%de", val)
	case int:
		fmt.Fprintf(buf, "i%de", val)
	case string:
		fmt.Fprintf(buf, "%d:", len(val))
		buf.WriteString(val)
	case []any:
		buf.WriteByte('l')
		for _, item := range val {
			if err := encodeBencodeValue(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte('e')
	case map[string]any:
		buf.WriteByte('d')
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(buf, "%d:%s", len(k), k)
			if err := encodeBencodeValue(buf, val[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('e')
	default:
		return fmt.Errorf("unsupported type: %T", v)
	}
	return nil
}
