package protocol

import (
	"encoding/hex"
)

// Keeper validates fingerprint grammar only. It never computes fingerprints:
// the instance-bound secret F stays on the CPA side and is never persisted or
// known by Keeper (contract section 3).

// FingerprintVector is one entry of the shared fingerprint-vectors fixture.
type FingerprintVector struct {
	RawKeyUtf8  string  `json:"rawKeyUtf8"`
	Fingerprint *string `json:"fingerprint"`
}

// FingerprintVectors is the typed cross-repo fingerprint vector object. It is
// not an HTTP body and therefore carries no protocolVersion.
type FingerprintVectors struct {
	FingerprintSecretHex string              `json:"fingerprintSecretHex"`
	Vectors              []FingerprintVector `json:"vectors"`
}

// DecodeFingerprintVectors strictly decodes the shared fingerprint vector
// fixture and validates every expected fingerprint's grammar.
func DecodeFingerprintVectors(data []byte) (*FingerprintVectors, *Error) {
	if _, perr := scanStrict(data, false); perr != nil {
		return nil, perr
	}
	var vectors FingerprintVectors
	if perr := decodeTyped(data, &vectors); perr != nil {
		return nil, perr
	}
	secret, err := hex.DecodeString(vectors.FingerprintSecretHex)
	if err != nil || len(secret) != 32 {
		return nil, protocolError("invalid_field")
	}
	for _, vector := range vectors.Vectors {
		if vector.Fingerprint != nil && !isFingerprint(*vector.Fingerprint) {
			return nil, protocolError("invalid_field")
		}
	}
	return &vectors, nil
}
