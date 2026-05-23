// Package idgen produces ULID-format document IDs for Den.
//
// IDs are 26-character Crockford base32 strings — 10 chars of 48-bit
// millisecond timestamp followed by 16 chars of 80-bit randomness — and
// are strictly monotonic within the same millisecond. The wire format
// matches the ULID spec at https://github.com/ulid/spec, so IDs are
// drop-in compatible with any external tooling that parses ULIDs.
//
// Monotonicity matters because Den uses the document ID as the default
// sort key (Sort("_id"), cursor pagination via After/Before). Two
// inserts in the same millisecond with random tails would sort
// unpredictably, occasionally letting a fresh row land "before" a
// previous cursor.
//
// The intra-millisecond step is a fresh 32-bit crypto/rand draw, not
// +1, so two consecutive IDs from the same millisecond don't reveal
// the next. Across millisecond boundaries the randomness is fully
// re-seeded.
package idgen

import (
	"crypto/rand"
	"encoding/binary"
	"sync"
	"time"
)

// Crockford base32 — https://www.crockford.com/base32.html
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// New returns a fresh ULID-format document ID. Safe for concurrent use.
func New() string {
	return defaultGen.new()
}

var defaultGen = newGenerator(nowMs)

func nowMs() uint64 {
	return uint64(time.Now().UnixMilli())
}

// A single mutex guards the counter state. DB I/O dominates any real
// workload so atomic/CAS optimisation isn't worth the complexity.
type generator struct {
	mu       sync.Mutex
	lastMs   uint64
	lastRand [10]byte
	now      func() uint64
}

func newGenerator(now func() uint64) *generator {
	return &generator{now: now}
}

func (g *generator) new() string {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now()
	if now > g.lastMs {
		g.lastMs = now
		seed(&g.lastRand)
	} else {
		// now == lastMs or now < lastMs (clock rewind): preserve
		// monotonicity by adding a fresh non-zero 32-bit crypto/rand
		// step. Random step (not +1) so two consecutive IDs from the
		// same ms don't leak the next. On overflow re-seed.
		if !addRandStep80(&g.lastRand) {
			seed(&g.lastRand)
		}
	}

	return encode(g.lastMs, g.lastRand)
}

func seed(b *[10]byte) {
	readRand(b[:])
}

func readRand(b []byte) {
	if _, err := rand.Read(b); err != nil {
		panic("idgen: crypto/rand failed: " + err.Error())
	}
}

// addRandStep80 adds a fresh non-zero 32-bit crypto/rand value to b
// (big-endian uint80). Returns false on overflow.
func addRandStep80(b *[10]byte) bool {
	var sb [4]byte
	readRand(sb[:])
	step := binary.BigEndian.Uint32(sb[:])
	if step == 0 {
		step = 1
	}
	carry := uint64(step)
	for i := 9; i >= 0 && carry > 0; i-- {
		sum := uint64(b[i]) + carry
		b[i] = byte(sum & 0xFF)
		carry = sum >> 8
	}
	return carry == 0
}

// encode produces the 26-char ULID string for the given timestamp and
// randomness. The first 10 chars carry the 48-bit timestamp (MSB
// first); the remaining 16 chars carry the 80-bit randomness in
// big-endian 5-bit groups.
func encode(ms uint64, r [10]byte) string {
	var buf [26]byte

	buf[0] = crockfordAlphabet[(ms>>45)&0x1F]
	buf[1] = crockfordAlphabet[(ms>>40)&0x1F]
	buf[2] = crockfordAlphabet[(ms>>35)&0x1F]
	buf[3] = crockfordAlphabet[(ms>>30)&0x1F]
	buf[4] = crockfordAlphabet[(ms>>25)&0x1F]
	buf[5] = crockfordAlphabet[(ms>>20)&0x1F]
	buf[6] = crockfordAlphabet[(ms>>15)&0x1F]
	buf[7] = crockfordAlphabet[(ms>>10)&0x1F]
	buf[8] = crockfordAlphabet[(ms>>5)&0x1F]
	buf[9] = crockfordAlphabet[ms&0x1F]

	buf[10] = crockfordAlphabet[(r[0]&0xF8)>>3]
	buf[11] = crockfordAlphabet[((r[0]&0x07)<<2)|((r[1]&0xC0)>>6)]
	buf[12] = crockfordAlphabet[(r[1]&0x3E)>>1]
	buf[13] = crockfordAlphabet[((r[1]&0x01)<<4)|((r[2]&0xF0)>>4)]
	buf[14] = crockfordAlphabet[((r[2]&0x0F)<<1)|((r[3]&0x80)>>7)]
	buf[15] = crockfordAlphabet[(r[3]&0x7C)>>2]
	buf[16] = crockfordAlphabet[((r[3]&0x03)<<3)|((r[4]&0xE0)>>5)]
	buf[17] = crockfordAlphabet[r[4]&0x1F]
	buf[18] = crockfordAlphabet[(r[5]&0xF8)>>3]
	buf[19] = crockfordAlphabet[((r[5]&0x07)<<2)|((r[6]&0xC0)>>6)]
	buf[20] = crockfordAlphabet[(r[6]&0x3E)>>1]
	buf[21] = crockfordAlphabet[((r[6]&0x01)<<4)|((r[7]&0xF0)>>4)]
	buf[22] = crockfordAlphabet[((r[7]&0x0F)<<1)|((r[8]&0x80)>>7)]
	buf[23] = crockfordAlphabet[(r[8]&0x7C)>>2]
	buf[24] = crockfordAlphabet[((r[8]&0x03)<<3)|((r[9]&0xE0)>>5)]
	buf[25] = crockfordAlphabet[r[9]&0x1F]

	return string(buf[:])
}
