package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"strings"
	"unicode"
)

func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func SimHash(text string) uint64 {
	words := tokenizeForHash(text)
	var vector [64]int
	for _, word := range words {
		h := fnv.New64a()
		_, _ = h.Write([]byte(strings.ToLower(word)))
		value := h.Sum64()
		for i := 0; i < 64; i++ {
			if value&(uint64(1)<<i) != 0 {
				vector[i]++
			} else {
				vector[i]--
			}
		}
	}

	var result uint64
	for i := 0; i < 64; i++ {
		if vector[i] > 0 {
			result |= uint64(1) << i
		}
	}
	return result
}

func SimHashString(text string) string {
	return fmt.Sprintf("%016x", SimHash(text))
}

func HammingDistanceHex(a, b string) int {
	var av, bv uint64
	_, _ = fmt.Sscanf(a, "%x", &av)
	_, _ = fmt.Sscanf(b, "%x", &bv)
	x := av ^ bv
	count := 0
	for x != 0 {
		count++
		x &= x - 1
	}
	return count
}

func tokenizeForHash(text string) []string {
	var words []string
	var current []rune
	flush := func() {
		if len(current) > 0 {
			words = append(words, string(current))
			current = nil
		}
	}

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r >= 0x4e00 && r <= 0x9fff {
			current = append(current, r)
			if r >= 0x4e00 && r <= 0x9fff && len(current) >= 2 {
				flush()
			}
			continue
		}
		flush()
	}
	flush()
	return words
}
