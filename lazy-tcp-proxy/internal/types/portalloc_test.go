package types

import (
	"testing"
)

func TestPortAllocator_BasicAllocation(t *testing.T) {
	a := NewPortAllocator(8000)
	got := a.AllocateForHosts([]TraefikHostSpec{{Domain: "s3.example.com", TargetPort: 9000}})
	if len(got) != 1 || got[0] != "s3.example.com:8000" {
		t.Errorf("got %v, want [s3.example.com:8000]", got)
	}
}

func TestPortAllocator_TwoHosts(t *testing.T) {
	a := NewPortAllocator(8000)
	got := a.AllocateForHosts([]TraefikHostSpec{
		{Domain: "a.com", TargetPort: 80},
		{Domain: "b.com", TargetPort: 443},
	})
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	if got[0] != "a.com:8000" {
		t.Errorf("got %q, want a.com:8000", got[0])
	}
	if got[1] != "b.com:8001" {
		t.Errorf("got %q, want b.com:8001", got[1])
	}
}

func TestPortAllocator_SkipsClaimedPort(t *testing.T) {
	a := NewPortAllocator(8000)
	a.ClaimPorts([]PortMapping{{ListenPort: 8000, TargetPort: 1234}})
	got := a.AllocateForHosts([]TraefikHostSpec{{Domain: "a.com", TargetPort: 80}})
	if len(got) != 1 || got[0] != "a.com:8001" {
		t.Errorf("got %v, want [a.com:8001]", got)
	}
}

func TestPortAllocator_StableReassignment(t *testing.T) {
	a := NewPortAllocator(8000)
	specs := []TraefikHostSpec{{Domain: "a.com", TargetPort: 80}}
	first := a.AllocateForHosts(specs)
	second := a.AllocateForHosts(specs)
	if len(first) != 1 || len(second) != 1 || first[0] != second[0] {
		t.Errorf("expected stable port: first=%v second=%v", first, second)
	}
}

func TestPortAllocator_ExplicitPortsNotOverlap(t *testing.T) {
	a := NewPortAllocator(8000)
	a.ClaimPorts([]PortMapping{{ListenPort: 8000, TargetPort: 9000}})
	got := a.AllocateForHosts([]TraefikHostSpec{{Domain: "b.com", TargetPort: 9000}})
	if len(got) != 1 || got[0] != "b.com:8001" {
		t.Errorf("got %v, want [b.com:8001]", got)
	}
}

func TestPortAllocator_EmptyDomainSkipped(t *testing.T) {
	a := NewPortAllocator(8000)
	got := a.AllocateForHosts([]TraefikHostSpec{{Domain: "", TargetPort: 9000}})
	if len(got) != 0 {
		t.Errorf("expected empty slice for blank domain, got %v", got)
	}
}

func TestPortAllocator_MixedEmptyAndValid(t *testing.T) {
	a := NewPortAllocator(8000)
	got := a.AllocateForHosts([]TraefikHostSpec{
		{Domain: "", TargetPort: 9000},
		{Domain: "real.com", TargetPort: 80},
	})
	if len(got) != 1 || got[0] != "real.com:8000" {
		t.Errorf("got %v, want [real.com:8000]", got)
	}
}
