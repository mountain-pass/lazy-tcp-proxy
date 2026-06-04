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

func TestAllocatePortMappings_SingleSpec(t *testing.T) {
	a := NewPortAllocator(8000)
	hosts, mappings := a.AllocatePortMappings([]TraefikHostSpec{{Domain: "a.com", TargetPort: 80}})
	if len(hosts) != 1 || hosts[0] != "a.com:8000" {
		t.Errorf("hosts: got %v, want [a.com:8000]", hosts)
	}
	if len(mappings) != 1 || mappings[0].ListenPort != 8000 || mappings[0].TargetPort != 80 {
		t.Errorf("mappings: got %v, want [{8000 80}]", mappings)
	}
}

func TestAllocatePortMappings_MultipleSpecsDifferentTargetPorts(t *testing.T) {
	a := NewPortAllocator(8000)
	hosts, mappings := a.AllocatePortMappings([]TraefikHostSpec{
		{Domain: "a.com", TargetPort: 80},
		{Domain: "b.com", TargetPort: 443},
	})
	if len(hosts) != 2 || hosts[0] != "a.com:8000" || hosts[1] != "b.com:8001" {
		t.Errorf("hosts: got %v", hosts)
	}
	if len(mappings) != 2 {
		t.Fatalf("mappings: expected 2, got %d", len(mappings))
	}
	if mappings[0] != (PortMapping{ListenPort: 8000, TargetPort: 80}) {
		t.Errorf("mappings[0]: got %v, want {8000 80}", mappings[0])
	}
	if mappings[1] != (PortMapping{ListenPort: 8001, TargetPort: 443}) {
		t.Errorf("mappings[1]: got %v, want {8001 443}", mappings[1])
	}
}

func TestAllocatePortMappings_SkipsClaimedPort(t *testing.T) {
	a := NewPortAllocator(8000)
	a.ClaimPorts([]PortMapping{{ListenPort: 8000, TargetPort: 9999}})
	_, mappings := a.AllocatePortMappings([]TraefikHostSpec{{Domain: "a.com", TargetPort: 80}})
	if len(mappings) != 1 || mappings[0].ListenPort != 8001 {
		t.Errorf("expected listen port 8001, got %v", mappings)
	}
}

func TestAllocatePortMappings_StableReallocation(t *testing.T) {
	a := NewPortAllocator(8000)
	specs := []TraefikHostSpec{{Domain: "a.com", TargetPort: 80}}
	_, first := a.AllocatePortMappings(specs)
	_, second := a.AllocatePortMappings(specs)
	if len(first) != 1 || len(second) != 1 || first[0] != second[0] {
		t.Errorf("expected stable mapping: first=%v second=%v", first, second)
	}
}

func TestAllocatePortMappings_EmptyDomainSkipped(t *testing.T) {
	a := NewPortAllocator(8000)
	hosts, mappings := a.AllocatePortMappings([]TraefikHostSpec{{Domain: "", TargetPort: 80}})
	if len(hosts) != 0 || len(mappings) != 0 {
		t.Errorf("expected empty results for blank domain, got hosts=%v mappings=%v", hosts, mappings)
	}
}
