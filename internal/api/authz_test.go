package api

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	_ "github.com/mcihad/kentos-izci/gen/izci/api/v1"
)

// TestEveryProcedureHasARule fails when an izci.api.v1 RPC has no
// authorization rule (it would be denied at runtime) or when a rule refers to
// an RPC that no longer exists.
func TestEveryProcedureHasARule(t *testing.T) {
	seen := map[string]bool{}
	protoregistry.GlobalFiles.RangeFilesByPackage("izci.api.v1", func(fd protoreflect.FileDescriptor) bool {
		for i := 0; i < fd.Services().Len(); i++ {
			svc := fd.Services().Get(i)
			for j := 0; j < svc.Methods().Len(); j++ {
				proc := "/" + string(svc.FullName()) + "/" + string(svc.Methods().Get(j).Name())
				seen[proc] = true
				if _, ok := rules[proc]; !ok {
					t.Errorf("no authorization rule for %s", proc)
				}
			}
		}
		return true
	})
	if len(seen) < 80 {
		t.Fatalf("found only %d procedures: the descriptor scan looks broken", len(seen))
	}
	for proc := range rules {
		if !seen[proc] {
			t.Errorf("rule for unknown procedure %s", proc)
		}
	}
}
