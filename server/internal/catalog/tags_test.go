package catalog

import (
	"context"
	"testing"
)

func TestEnsureTagFailsWhenLibraryIsUnavailable(t *testing.T) {
	s, _ := newService(t)
	s.DB.Close()
	if _, _, err := s.EnsureTag(context.Background(), 1, "04A1"); err == nil {
		t.Fatal("expected an error from a closed database")
	}
}

func TestEnsureTagFailsWhenTagCannotBeStored(t *testing.T) {
	s, _ := newService(t)
	if _, _, err := s.EnsureTag(context.Background(), 99, "04A1"); err == nil {
		t.Fatal("tag stored for a user that does not exist")
	}
}
