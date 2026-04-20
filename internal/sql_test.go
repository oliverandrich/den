package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEscapeLike_NoSpecialChars(t *testing.T) {
	assert.Equal(t, "hello world", EscapeLike("hello world"))
	assert.Empty(t, EscapeLike(""))
}

func TestEscapeLike_Percent(t *testing.T) {
	assert.Equal(t, `50\%`, EscapeLike("50%"))
	assert.Equal(t, `\%foo\%`, EscapeLike("%foo%"))
}

func TestEscapeLike_Underscore(t *testing.T) {
	assert.Equal(t, `a\_b`, EscapeLike("a_b"))
	assert.Equal(t, `\_\_\_`, EscapeLike("___"))
}

func TestEscapeLike_Backslash(t *testing.T) {
	assert.Equal(t, `a\\b`, EscapeLike(`a\b`))
	assert.Equal(t, `\\\\`, EscapeLike(`\\`))
}

func TestEscapeLike_BackslashOrderMatters(t *testing.T) {
	// Order is critical: backslash must escape FIRST. If % escaped first for
	// input "\%", step 1 would produce "\\%", then the backslash pass would
	// double BOTH backslashes to "\\\\%" — four backslashes before the %,
	// which a LIKE engine reads as two literal backslashes followed by a
	// wildcard percent. The test value below locks in the correct order.
	assert.Equal(t, `\\\%`, EscapeLike(`\%`))
	assert.Equal(t, `\\\_`, EscapeLike(`\_`))
}

func TestEscapeLike_AllSpecialCharsCombined(t *testing.T) {
	assert.Equal(t, `a\\b\%c\_d`, EscapeLike(`a\b%c_d`))
}

func TestEscapeLike_SingleQuotesPassThrough(t *testing.T) {
	// Single quotes are not LIKE-special; they only matter in raw SQL string
	// literals. Den uses parameter binding, so the value travels unchanged.
	assert.Equal(t, `it's a "test"`, EscapeLike(`it's a "test"`))
}

func TestEscapeLike_Unicode(t *testing.T) {
	// Multi-byte chars are not LIKE-special; they pass through untouched.
	assert.Equal(t, "café", EscapeLike("café"))
	assert.Equal(t, "日本語", EscapeLike("日本語"))
	assert.Equal(t, "🎉👋", EscapeLike("🎉👋"))
}

func TestEscapeLike_PreservesWhitespace(t *testing.T) {
	assert.Equal(t, "  foo  ", EscapeLike("  foo  "))
	assert.Equal(t, "\t\nfoo\r", EscapeLike("\t\nfoo\r"))
}
