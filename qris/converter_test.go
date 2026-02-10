package qris

import (
	"strings"
	"testing"
)

func TestConvertBasic(t *testing.T) {
	// Minimal static QRIS string
	// Tag 00: payload format "01"
	// Tag 01: point of initiation "11" (static)
	// Tag 53: currency "360" (IDR)
	// Tag 58: country "ID"
	// Tag 59: merchant name "TEST"
	// Tag 60: city "JAKARTA"
	// Tag 63: CRC (will be calculated)
	input := "00020101021153033605802ID5904TEST6007JAKARTA"
	// Add CRC to make it valid
	crcData := input + "6304"
	crc := CRC16CCITT(crcData)
	input = crcData + crc

	result, err := Convert(input, 50000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify tag 01 is now "12" (dynamic)
	tlvs, err := Parse(result.DynamicQRIS)
	if err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	var foundTag01, foundTag54, foundTag63 bool
	for _, tlv := range tlvs {
		switch tlv.Tag {
		case "01":
			foundTag01 = true
			if tlv.Value != "12" {
				t.Errorf("tag 01: expected '12', got '%s'", tlv.Value)
			}
		case "54":
			foundTag54 = true
			if tlv.Value != "50000" {
				t.Errorf("tag 54: expected '50000', got '%s'", tlv.Value)
			}
		case "63":
			foundTag63 = true
			if len(tlv.Value) != 4 {
				t.Errorf("tag 63: expected 4-char CRC, got '%s'", tlv.Value)
			}
		}
	}

	if !foundTag01 {
		t.Error("tag 01 not found in result")
	}
	if !foundTag54 {
		t.Error("tag 54 not found in result")
	}
	if !foundTag63 {
		t.Error("tag 63 not found in result")
	}
}

func TestConvertVerifyCRC(t *testing.T) {
	input := "00020101021153033605802ID5904TEST6007JAKARTA"
	crcData := input + "6304"
	crc := CRC16CCITT(crcData)
	input = crcData + crc

	result, err := Convert(input, 25000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify CRC is correct by parsing, removing CRC, recalculating
	qris := result.DynamicQRIS
	// Last 4 chars are the CRC value
	if !strings.Contains(qris, "6304") {
		t.Fatal("CRC tag 63 not found in output")
	}

	crcIdx := strings.LastIndex(qris, "6304")
	dataForCRC := qris[:crcIdx+4]
	expectedCRC := CRC16CCITT(dataForCRC)
	actualCRC := qris[crcIdx+4:]

	if actualCRC != expectedCRC {
		t.Errorf("CRC mismatch: expected '%s', got '%s'", expectedCRC, actualCRC)
	}
}

func TestConvertInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		qris   string
		amount int
	}{
		{"empty string", "", 50000},
		{"too short", "0002", 50000},
		{"zero amount", "00020101021153033605802ID630400000000", 0},
		{"negative amount", "00020101021153033605802ID630400000000", -100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Convert(tt.qris, tt.amount)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestConvertAlreadyHasAmount(t *testing.T) {
	// Test that if tag 54 already exists, it gets replaced
	// tag 00=01, tag 01=11, tag 53=360, tag 54=10000, tag 58=ID, tag 59=TEST, tag 60=JAKARTA
	input := "00020101021153033605405100005802ID5904TEST6007JAKARTA"
	crcData := input + "6304"
	crc := CRC16CCITT(crcData)
	input = crcData + crc

	result, err := Convert(input, 75000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tlvs, err := Parse(result.DynamicQRIS)
	if err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	amountCount := 0
	for _, tlv := range tlvs {
		if tlv.Tag == "54" {
			amountCount++
			if tlv.Value != "75000" {
				t.Errorf("tag 54: expected '75000', got '%s'", tlv.Value)
			}
		}
	}

	if amountCount != 1 {
		t.Errorf("expected exactly 1 tag 54, found %d", amountCount)
	}
}
