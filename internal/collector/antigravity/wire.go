package antigravity

// Minimal, schema-less protobuf wire-format reader.
//
// Callers explicitly select nested message paths and text fields. The reader
// does not guess field types or decode prompt content.
//
// The reader is lazy: nothing is materialised into a tree, a lookup just walks
// the buffer skipping over fields it does not care about. Decoding a 160 KB row
// to fetch 1.4.2 therefore never copies the 160 KB of prompt text.
//
// Every function is bounds checked and returns errMalformed instead of
// panicking on garbage input (the CLI may be mid-write, or change format).

import (
	"encoding/binary"
	"errors"
)

var errMalformed = errors.New("antigravity: malformed protobuf blob")

// protobuf wire types we accept. 3/4 (start/end group) are legacy and never
// appear in these blobs; seeing one means we lost sync, i.e. malformed.
const (
	wireVarint  = 0
	wireFixed64 = 1
	wireBytes   = 2
	wireFixed32 = 5
)

// maxDepth caps how deep a path may descend. The deepest path in the field map
// is three levels (1.4.11, 1.9.4.1), the cap only exists so that a corrupted
// blob can never drive unbounded work.
const maxDepth = 8

// maxFieldNum is protobuf's largest legal field number (2^29-1).
const maxFieldNum = 1<<29 - 1

// field is one decoded wire field. For wire types 0/1/5 the payload is in num
// form in val; for wire type 2 it is the (aliased, not copied) slice buf.
type field struct {
	num  int
	wire int
	val  uint64
	buf  []byte
}

// readVarint decodes a base-128 varint starting at i.
func readVarint(b []byte, i int) (v uint64, next int, err error) {
	var shift uint
	for n := 0; n < 10; n++ {
		if i >= len(b) {
			return 0, 0, errMalformed
		}
		c := b[i]
		i++
		if n == 9 && c > 1 { // 10th byte may only carry the 64th bit
			return 0, 0, errMalformed
		}
		v |= uint64(c&0x7f) << shift
		if c < 0x80 {
			return v, i, nil
		}
		shift += 7
	}
	return 0, 0, errMalformed
}

// forEachField walks the fields of a message body in order. fn returns false to
// stop early. A malformed buffer stops the walk with errMalformed.
func forEachField(buf []byte, fn func(f field) bool) error {
	for i := 0; i < len(buf); {
		key, next, err := readVarint(buf, i)
		if err != nil {
			return err
		}
		i = next
		num := int(key >> 3)
		if num <= 0 || num > maxFieldNum {
			return errMalformed
		}
		f := field{num: num, wire: int(key & 7)}
		switch f.wire {
		case wireVarint:
			if f.val, i, err = readVarint(buf, i); err != nil {
				return err
			}
		case wireFixed64:
			if len(buf)-i < 8 {
				return errMalformed
			}
			f.val = binary.LittleEndian.Uint64(buf[i:])
			i += 8
		case wireFixed32:
			if len(buf)-i < 4 {
				return errMalformed
			}
			f.val = uint64(binary.LittleEndian.Uint32(buf[i:]))
			i += 4
		case wireBytes:
			n, next, err := readVarint(buf, i)
			if err != nil {
				return err
			}
			if n > uint64(len(buf)-next) {
				return errMalformed
			}
			f.buf = buf[next : next+int(n)]
			i = next + int(n)
		default: // 3, 4 (groups), 6, 7 (reserved)
			return errMalformed
		}
		if !fn(f) {
			return nil
		}
	}
	return nil
}

// message is a protobuf message body (the bytes between its delimiters).
type message []byte

// find returns the first field with the given number.
func (m message) find(num int) (field, bool, error) {
	var found field
	var ok bool
	err := forEachField(m, func(f field) bool {
		if f.num == num {
			found, ok = f, true
			return false
		}
		return true
	})
	if err != nil {
		return field{}, false, err
	}
	return found, ok, nil
}

// resolve walks path, descending through length-delimited fields, and returns
// the field named by the last element. Callers must only pass paths whose
// intermediate elements the collector treats as nested messages.
func (m message) resolve(path ...int) (field, bool, error) {
	if len(path) == 0 || len(path) > maxDepth {
		return field{}, false, errMalformed
	}
	cur := m
	for i, num := range path {
		f, ok, err := cur.find(num)
		if err != nil || !ok {
			return field{}, false, err
		}
		if i == len(path)-1 {
			return f, true, nil
		}
		if f.wire != wireBytes {
			return field{}, false, nil // not a message: treat as absent
		}
		cur = f.buf
	}
	return field{}, false, errMalformed
}

// sub returns the nested message at path (e.g. sub(1, 4) for the usage struct).
func (m message) sub(path ...int) (message, bool, error) {
	f, ok, err := m.resolve(path...)
	if err != nil || !ok || f.wire != wireBytes {
		return nil, false, err
	}
	return f.buf, true, nil
}

// uint returns the varint (or fixed) at path. The bool distinguishes "absent"
// from "present and zero", which matters for 1.4.9 (thoughts): Claude rows omit
// the field entirely, Gemini rows may legitimately carry 0.
func (m message) uint(path ...int) (uint64, bool, error) {
	f, ok, err := m.resolve(path...)
	if err != nil || !ok {
		return 0, false, err
	}
	switch f.wire {
	case wireVarint, wireFixed64, wireFixed32:
		return f.val, true, nil
	default:
		return 0, false, nil
	}
}

// int64At is uint clamped into int64; negative/absurd values become 0 so a
// corrupt row can never produce a negative token count.
func (m message) int64At(path ...int) (int64, bool, error) {
	v, ok, err := m.uint(path...)
	if err != nil || !ok {
		return 0, false, err
	}
	if v > 1<<62 {
		return 0, false, nil
	}
	return int64(v), true, nil
}

// str returns the length-delimited field at path as text. Only used for paths
// the field map marks as strings (4, 1.19, 1.4.11, kv keys, 20.4).
func (m message) str(path ...int) (string, bool, error) {
	f, ok, err := m.resolve(path...)
	if err != nil || !ok || f.wire != wireBytes {
		return "", false, err
	}
	return string(f.buf), true, nil
}

// eachSub calls fn for every occurrence of a repeated nested message field
// (used for the 1.20 key/value list).
func (m message) eachSub(num int, fn func(message) bool) error {
	return forEachField(m, func(f field) bool {
		if f.num != num || f.wire != wireBytes {
			return true
		}
		return fn(f.buf)
	})
}
