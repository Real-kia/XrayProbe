package netutil

import "testing"

func TestDefaultInterfaceName(t *testing.T) {
	name, err := DefaultInterfaceName()
	if err != nil {
		t.Skipf("no default route available in this environment: %v", err)
	}
	if name == "" {
		t.Fatal("DefaultInterfaceName returned an empty name with no error")
	}
}
