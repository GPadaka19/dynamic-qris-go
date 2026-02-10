package qris

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	TagPayloadFormat       = "00"
	TagPointOfInitiation   = "01"
	TagMerchantName        = "59"
	TagMerchantCity        = "60"
	TagTransactionCurrency = "53"
	TagTransactionAmount   = "54"
	TagCountryCode         = "58"
	TagCRC                 = "63"

	StaticQRIS  = "11"
	DynamicQRIS = "12"
)

// ConvertResult holds the result of a QRIS conversion.
type ConvertResult struct {
	DynamicQRIS string `json:"dynamic_qris"`
}

// Convert takes a static QRIS string and an amount, and returns a dynamic QRIS string.
func Convert(qrisString string, amount int) (*ConvertResult, error) {
	qrisString = strings.TrimSpace(qrisString)

	if len(qrisString) < 10 {
		return nil, fmt.Errorf("QRIS string too short")
	}

	// Parse original TLV data
	tlvs, err := Parse(qrisString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse QRIS: %w", err)
	}

	if len(tlvs) == 0 {
		return nil, fmt.Errorf("no TLV data found in QRIS string")
	}

	// Validate it starts with payload format indicator (tag 00)
	if tlvs[0].Tag != TagPayloadFormat {
		return nil, fmt.Errorf("invalid QRIS: missing payload format indicator (tag 00)")
	}

	if amount <= 0 {
		return nil, fmt.Errorf("amount must be greater than 0")
	}

	// Check if tag 54 already exists in the original
	hasExistingAmount := false
	for _, tlv := range tlvs {
		if tlv.Tag == TagTransactionAmount {
			hasExistingAmount = true
			break
		}
	}

	// Build new TLV list
	var newTLVs []TLV
	amountInserted := false
	amountStr := strconv.Itoa(amount)

	for _, tlv := range tlvs {
		switch tlv.Tag {
		case TagPointOfInitiation:
			// Change from static (11) to dynamic (12)
			newTLVs = append(newTLVs, TLV{Tag: TagPointOfInitiation, Value: DynamicQRIS})

		case TagTransactionAmount:
			// Replace existing amount with new value
			newTLVs = append(newTLVs, TLV{Tag: TagTransactionAmount, Value: amountStr})
			amountInserted = true

		case TagCRC:
			// Skip old CRC — we'll recalculate it
			continue

		default:
			newTLVs = append(newTLVs, tlv)
		}

		// Insert amount after transaction currency (tag 53) only if tag 54 doesn't already exist
		if tlv.Tag == TagTransactionCurrency && !amountInserted && !hasExistingAmount {
			newTLVs = append(newTLVs, TLV{Tag: TagTransactionAmount, Value: amountStr})
			amountInserted = true
		}
	}

	// If amount still not inserted (no tag 53 and no tag 54 found), append
	if !amountInserted {
		newTLVs = append(newTLVs, TLV{Tag: TagTransactionAmount, Value: amountStr})
	}

	// Add CRC placeholder and calculate
	newTLVs = append(newTLVs, TLV{Tag: TagCRC, Value: "0000"})

	// Serialize without final CRC value to calculate checksum
	serialized := Serialize(newTLVs)
	// Remove the placeholder "0000" at the end to get the data to checksum
	dataForCRC := serialized[:len(serialized)-4]
	crc := CRC16CCITT(dataForCRC)

	// Replace placeholder with actual CRC
	newTLVs[len(newTLVs)-1] = TLV{Tag: TagCRC, Value: crc}

	return &ConvertResult{
		DynamicQRIS: Serialize(newTLVs),
	}, nil
}
