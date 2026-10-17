package crypto

import (
	"crypto/sha256"

	"github.com/RishiKendai/chunkvault/internals/types"
)

type IDer interface {
	ID(plain []byte) types.ChunkID
}

type SHA256IDer struct{}

func (SHA256IDer) ID(p []byte) types.ChunkID { return sha256.Sum256(p) }
