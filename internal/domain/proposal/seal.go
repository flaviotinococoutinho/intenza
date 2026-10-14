package proposal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/flaviotinococoutinho/intenza/internal/domain/money"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso4217"
)

var (
	ErrSealKey   = errors.New("proposal: seal key must have 32 bytes")
	ErrSealOpen  = errors.New("proposal: sealed price does not open (wrong key or tampered)")
	ErrSealedNow = errors.New("proposal: prices stay sealed until the intent is closed")
)

// Sealed is a price encrypted at rest. Nothing outside the sealer can read it,
// so a leaked document, log line or event carries no price.
type Sealed struct {
	KeyVersion int    `json:"key_version"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

// Sealer encrypts prices with AES-256-GCM under a key derived per intent
// (HKDF-SHA256), binding intent, proposal, seller and revision as associated
// data: a ciphertext moved to another proposal does not open.
type Sealer struct {
	masters map[int][]byte
	current int
	entropy io.Reader
}

// NewSealer takes versioned master keys (rotation keeps old versions for
// reading) and the entropy source for nonces.
func NewSealer(masters map[int][]byte, current int, entropy io.Reader) (*Sealer, error) {
	for _, k := range masters {
		if len(k) != 32 {
			return nil, ErrSealKey
		}
	}
	if _, ok := masters[current]; !ok {
		return nil, fmt.Errorf("%w: versão corrente %d ausente", ErrSealKey, current)
	}
	return &Sealer{masters: masters, current: current, entropy: entropy}, nil
}

func (s *Sealer) aead(version int, intentID string) (cipher.AEAD, error) {
	master, ok := s.masters[version]
	if !ok {
		return nil, ErrSealOpen
	}
	key, err := hkdf.Key(sha256.New, master, nil, "intenza/proposal-price/v1/"+intentID, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func aad(intentID, proposalID, sellerID string, revision int) []byte {
	return []byte(intentID + "|" + proposalID + "|" + sellerID + "|" + strconv.Itoa(revision))
}

// Seal encrypts the price.
func (s *Sealer) Seal(intentID, proposalID, sellerID string, revision int, price money.Money) (Sealed, error) {
	g, err := s.aead(s.current, intentID)
	if err != nil {
		return Sealed{}, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := io.ReadFull(s.entropy, nonce); err != nil {
		return Sealed{}, err
	}
	var plain [10]byte
	binary.BigEndian.PutUint64(plain[:8], uint64(price.Minor))
	binary.BigEndian.PutUint16(plain[8:], uint16(price.Currency.Numeric()))
	return Sealed{KeyVersion: s.current, Nonce: nonce, Ciphertext: g.Seal(nil, nonce, plain[:], aad(intentID, proposalID, sellerID, revision))}, nil
}

// open decrypts. It is unexported on purpose: the only way in is Reveal,
// which checks the persisted phase of the intent first.
func (s *Sealer) open(intentID, proposalID, sellerID string, revision int, sealed Sealed) (money.Money, error) {
	g, err := s.aead(sealed.KeyVersion, intentID)
	if err != nil {
		return money.Money{}, err
	}
	plain, err := g.Open(nil, sealed.Nonce, sealed.Ciphertext, aad(intentID, proposalID, sellerID, revision))
	if err != nil || len(plain) != 10 {
		return money.Money{}, ErrSealOpen
	}
	cur, ok := iso4217.ByNumeric(int(binary.BigEndian.Uint16(plain[8:])))
	if !ok {
		return money.Money{}, ErrSealOpen
	}
	return money.Money{Minor: int64(binary.BigEndian.Uint64(plain[:8])), Currency: cur}, nil
}
