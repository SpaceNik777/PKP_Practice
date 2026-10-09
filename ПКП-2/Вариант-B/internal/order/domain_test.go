package order

import (
	"errors"
	"testing"
)

func TestNewValidatesLines(t *testing.T) {
	tests := []struct {
		name  string
		id    string
		lines []Line
	}{
		{name: "empty id", lines: []Line{{SKU: "A", Quantity: 1, UnitCents: 100}}},
		{name: "no lines", id: "ord-1"},
		{name: "empty sku", id: "ord-1", lines: []Line{{Quantity: 1, UnitCents: 100}}},
		{name: "zero quantity", id: "ord-1", lines: []Line{{SKU: "A", UnitCents: 100}}},
		{name: "zero price", id: "ord-1", lines: []Line{{SKU: "A", Quantity: 1}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.id, tt.lines)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestOrderComputesTotalAndProtectsLines(t *testing.T) {
	source := []Line{{SKU: "BOOK-GO", Quantity: 2, UnitCents: 2500}}
	got, err := New("ord-1001", source)
	if err != nil {
		t.Fatal(err)
	}

	source[0].Quantity = 999
	returned := got.Lines()
	returned[0].Quantity = 777

	if got.TotalCents() != 5000 {
		t.Fatalf("total = %d, want 5000", got.TotalCents())
	}
}

func TestOrderCannotBeConfirmedTwice(t *testing.T) {
	got, err := New("ord-1001", []Line{{SKU: "A", Quantity: 1, UnitCents: 100}})
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Confirm(); err != nil {
		t.Fatal(err)
	}
	if err := got.Confirm(); !errors.Is(err, ErrConflict) {
		t.Fatalf("error = %v, want ErrConflict", err)
	}
}
