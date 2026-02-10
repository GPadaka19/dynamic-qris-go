package qris

import (
	"testing"
)

func TestParse(t *testing.T) {
	// tag=00 len=02 val=01, tag=01 len=02 val=11, tag=53 len=03 val=360
	raw := "0002010102115303360"
	tlvs, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tlvs) != 3 {
		t.Fatalf("expected 3 TLVs, got %d", len(tlvs))
	}

	// Tag 00
	if tlvs[0].Tag != "00" || tlvs[0].Value != "01" {
		t.Errorf("tag 00: expected value '01', got '%s'", tlvs[0].Value)
	}

	// Tag 01
	if tlvs[1].Tag != "01" || tlvs[1].Value != "11" {
		t.Errorf("tag 01: expected value '11', got '%s'", tlvs[1].Value)
	}

	// Tag 53
	if tlvs[2].Tag != "53" || tlvs[2].Value != "360" {
		t.Errorf("tag 53: expected value '360', got '%s'", tlvs[2].Value)
	}
}

func TestParseAndSerializeRoundtrip(t *testing.T) {
	// Minimal QRIS-like string
	raw := "00020101021153033605802ID"
	tlvs, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	serialized := Serialize(tlvs)
	if serialized != raw {
		t.Errorf("roundtrip failed:\n  input:  %s\n  output: %s", raw, serialized)
	}
}

func TestParseInvalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"too short", "00"},
		{"bad length", "00XX01"},
		{"length exceeds data", "000501"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.input)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestCRC16CCITT(t *testing.T) {
	// Known test vector: "123456789" should produce CRC16-CCITT of 0x29B1
	crc := CRC16CCITT("123456789")
	if crc != "29B1" {
		t.Errorf("expected CRC '29B1', got '%s'", crc)
	}
}

func TestCRC16CCITTForQRIS(t *testing.T) {
	// A QRIS CRC is calculated over the entire data string including the "6304" prefix
	// but excluding the 4-char CRC value itself
	data := "00020101021126"
	// Just verify it returns a 4-char hex string
	crc := CRC16CCITT(data + "6304")
	if len(crc) != 4 {
		t.Errorf("expected 4-char CRC, got '%s' (len=%d)", crc, len(crc))
	}
}

func TestTLVLength(t *testing.T) {
	tlv := TLV{Tag: "54", Value: "50000"}
	if tlv.Length() != "05" {
		t.Errorf("expected length '05', got '%s'", tlv.Length())
	}
}

func TestTLVString(t *testing.T) {
	tlv := TLV{Tag: "54", Value: "50000"}
	expected := "540550000"
	if tlv.String() != expected {
		t.Errorf("expected '%s', got '%s'", expected, tlv.String())
	}
}
