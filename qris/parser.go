package qris

import (
	"fmt"
	"strconv"
	"strings"
)

// TLV represents a Tag-Length-Value data object in QRIS/EMVCo format.
type TLV struct {
	Tag   string
	Value string
}

// Length returns the length of the value as a zero-padded string.
func (t TLV) Length() string {
	return fmt.Sprintf("%02d", len(t.Value))
}

// String serializes a single TLV to its string representation.
func (t TLV) String() string {
	return t.Tag + t.Length() + t.Value
}

// Parse parses a raw QRIS string into a slice of TLV data objects.
func Parse(raw string) ([]TLV, error) {
	var tlvs []TLV
	i := 0

	for i < len(raw) {
		// Need at least 4 characters: 2 for tag + 2 for length
		if i+4 > len(raw) {
			return nil, fmt.Errorf("invalid TLV at position %d: insufficient data", i)
		}

		tag := raw[i : i+2]
		lengthStr := raw[i+2 : i+4]

		length, err := strconv.Atoi(lengthStr)
		if err != nil {
			return nil, fmt.Errorf("invalid length '%s' at position %d", lengthStr, i+2)
		}

		if i+4+length > len(raw) {
			return nil, fmt.Errorf("invalid TLV at position %d: value length %d exceeds remaining data", i, length)
		}

		value := raw[i+4 : i+4+length]
		tlvs = append(tlvs, TLV{Tag: tag, Value: value})

		i += 4 + length
	}

	return tlvs, nil
}

// Serialize converts a slice of TLV objects back to a QRIS string.
func Serialize(tlvs []TLV) string {
	var sb strings.Builder
	for _, tlv := range tlvs {
		sb.WriteString(tlv.String())
	}
	return sb.String()
}

// CRC16CCITT computes the CRC16-CCITT checksum used in QRIS/EMVCo.
// The polynomial is 0x1021 with an initial value of 0xFFFF.
func CRC16CCITT(data string) string {
	crc := uint16(0xFFFF)

	for i := 0; i < len(data); i++ {
		crc ^= uint16(data[i]) << 8
		for j := 0; j < 8; j++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}

	return fmt.Sprintf("%04X", crc)
}
