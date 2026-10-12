package postgres

import (
	"reflect"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestUUIDCodecRoundTripsStdlibUUID(t *testing.T) {
	codec := pgtype.NewMap()
	want := uuid.NewV7()
	encoded, err := codec.Encode(pgtype.UUIDOID, pgtype.BinaryFormatCode, want, nil)
	if err != nil {
		t.Fatalf("encode UUID: %v", err)
	}

	var got uuid.UUID
	if err := codec.Scan(pgtype.UUIDOID, pgtype.BinaryFormatCode, encoded, &got); err != nil {
		t.Fatalf("scan UUID: %v", err)
	}
	if got != want {
		t.Fatalf("UUID round trip = %s, want %s", got, want)
	}
}

func TestUUIDCodecRoundTripsNullableUUID(t *testing.T) {
	codec := pgtype.NewMap()
	want := uuid.NewV7()
	encoded, err := codec.Encode(pgtype.UUIDOID, pgtype.BinaryFormatCode, &want, nil)
	if err != nil {
		t.Fatalf("encode nullable UUID: %v", err)
	}

	var got *uuid.UUID
	if err := codec.Scan(pgtype.UUIDOID, pgtype.BinaryFormatCode, encoded, &got); err != nil {
		t.Fatalf("scan nullable UUID: %v", err)
	}
	if got == nil || *got != want {
		t.Fatalf("nullable UUID round trip = %v, want %s", got, want)
	}

	var absent *uuid.UUID
	encoded, err = codec.Encode(pgtype.UUIDOID, pgtype.BinaryFormatCode, absent, nil)
	if err != nil {
		t.Fatalf("encode null UUID: %v", err)
	}
	got = nil
	if err := codec.Scan(pgtype.UUIDOID, pgtype.BinaryFormatCode, encoded, &got); err != nil {
		t.Fatalf("scan null UUID: %v", err)
	}
	if got != nil {
		t.Fatalf("null UUID scanned as %s", *got)
	}
}

func TestUUIDArrayCodecRoundTripsStdlibUUIDs(t *testing.T) {
	codec := pgtype.NewMap()
	want := []uuid.UUID{uuid.NewV7(), uuid.NewV7()}
	encoded, err := codec.Encode(pgtype.UUIDArrayOID, pgtype.BinaryFormatCode, want, nil)
	if err != nil {
		t.Fatalf("encode UUID array: %v", err)
	}

	var got []uuid.UUID
	if err := codec.Scan(pgtype.UUIDArrayOID, pgtype.BinaryFormatCode, encoded, &got); err != nil {
		t.Fatalf("scan UUID array: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UUID array round trip = %v, want %v", got, want)
	}
}
