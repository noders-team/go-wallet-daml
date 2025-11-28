package crypto

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
)

const (
	CantonHashPurposeTopology = 1
)

func ComputeSHA256CantonHash(purpose int, data []byte) ([]byte, error) {
	h := sha256.New()

	purposeBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(purposeBytes, uint32(purpose))

	if _, err := h.Write(purposeBytes); err != nil {
		return nil, fmt.Errorf("failed to write purpose to hash: %w", err)
	}

	if _, err := h.Write(data); err != nil {
		return nil, fmt.Errorf("failed to write data to hash: %w", err)
	}

	return h.Sum(nil), nil
}

func ComputeMultiHashForTopology(hashes [][]byte) ([]byte, error) {
	h := sha256.New()

	for _, hash := range hashes {
		if _, err := h.Write(hash); err != nil {
			return nil, fmt.Errorf("failed to write hash to multi-hash: %w", err)
		}
	}

	combinedHash := h.Sum(nil)

	return ComputeSHA256CantonHash(CantonHashPurposeTopology, combinedHash)
}

func HashPreparedTransaction(preparedTransactionBase64 string) (string, error) {
	preparedBytes, err := base64.StdEncoding.DecodeString(preparedTransactionBase64)
	if err != nil {
		return "", fmt.Errorf("failed to decode prepared transaction: %w", err)
	}

	hash := sha256.Sum256(preparedBytes)
	return base64.StdEncoding.EncodeToString(hash[:]), nil
}
