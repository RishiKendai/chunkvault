package types

import (
	"encoding/hex"
	"fmt"
)

type ChunkID [32]byte

func (id ChunkID) String() string { return hex.EncodeToString(id[:]) }

func ParseChunkID(s string) (ChunkID, error) {
	var id ChunkID
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != len(id) {
		return id, fmt.Errorf("invalid chunk id %q", s)
	}
	copy(id[:], b)
	return id, nil
}
