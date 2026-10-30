package checker

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"strings"
	"unicode"
	"unicode/utf8"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func generateFlag(teamID uuid.UUID, task domain.Task, round int32, place int32) (domain.Flag, error) {
	const location = "checker.generateFlag"
	if task.Name == "" || place < 1 || place > task.Places {
		return domain.Flag{}, fmt.Errorf("%s - task name and valid place are required for PUT", location)
	}
	r, _ := utf8.DecodeRuneInString(task.Name)
	prefix := unicode.ToUpper(r)
	isLetter := prefix >= 'A' && prefix <= 'Z'
	isDigit := prefix >= '0' && prefix <= '9'
	if !isLetter && !isDigit {
		return domain.Flag{}, fmt.Errorf("%s - task name must start with an ASCII letter or digit", location)
	}
	var randomFlag strings.Builder
	randomFlag.Grow(32)
	randomFlag.WriteRune(prefix)
	for range 30 {
		index, err := randomBelow(int64(len(flagAlphabet)))
		if err != nil {
			return domain.Flag{}, fmt.Errorf("%s - generate random flag: %w", location, err)
		}
		randomFlag.WriteByte(flagAlphabet[int(index)])
	}
	randomFlag.WriteByte('=')
	seed := make([]byte, 20)
	if _, err := io.ReadFull(cryptorand.Reader, seed); err != nil {
		return domain.Flag{}, fmt.Errorf("%s - generate private flag seed: %w", location, err)
	}
	return domain.Flag{
		Value: randomFlag.String(), TeamID: teamID, TaskID: task.ID, Round: round,
		PrivateData: hex.EncodeToString(seed), VulnerabilityID: &place,
	}, nil
}

func flagForStorage(flag domain.Flag, task domain.Task, v verdict) domain.Flag {
	switch {
	case hasTag(task.CheckerType, "nfr"):
		flag.PublicData = ""
	case hasTag(task.CheckerType, "pfr"):
		flag.PublicData = v.public
		flag.PrivateData = v.private
	default:
		flag.PublicData = ""
		flag.PrivateData = v.public
	}
	return flag
}

func hasTag(tags, tag string) bool {
	for _, item := range strings.Split(tags, "_") {
		if item == tag {
			return true
		}
	}
	return false
}

func randomBelow(limit int64) (int64, error) {
	const location = "checker.randomBelow"
	if limit <= 0 {
		return 0, fmt.Errorf("%s - random limit must be positive", location)
	}
	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(limit))
	if err != nil {
		return 0, fmt.Errorf("%s - generate random integer: %w", location, err)
	}
	return n.Int64(), nil
}

func randomFlagRound(firstRound int32, width int64) (int32, bool) {
	choice, err := randomBelow(width)
	if err != nil {
		return 0, false
	}
	round := int64(firstRound) + choice
	if round < 1 || round > maxCheckerRound {
		return 0, false
	}
	return int32(round), true
}
