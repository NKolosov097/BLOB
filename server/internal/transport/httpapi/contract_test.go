package httpapi

import (
	"context"
	"testing"
)

func TestEmbeddedContractIsValid(t *testing.T) {
	spec, err := GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	if err := spec.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
}
