package types

import (
	"net"
	"testing"
	"time"
)

// ---- ParsePortMappings ----

func TestParsePortMappings_Valid(t *testing.T) {
	got := ParsePortMappings("test", "9000:80,9001:8080")
	if len(got) != 2 {
		t.Fatalf("expected 2 mappings, got %d", len(got))
	}
	if got[0].ListenPort != 9000 || got[0].TargetPort != 80 {
		t.Errorf("mapping 0: got %+v, want {9000 80}", got[0])
	}
	if got[1].ListenPort != 9001 || got[1].TargetPort != 8080 {
		t.Errorf("mapping 1: got %+v, want {9001 8080}", got[1])
	}
}

func TestParsePortMappings_SingleMapping(t *testing.T) {
	got := ParsePortMappings("test", "5353:53")
	if len(got) != 1 {
		t.Fatalf("expected 1 mapping, got %d", len(got))
	}
	if got[0].ListenPort != 5353 || got[0].TargetPort != 53 {
		t.Errorf("got %+v, want {5353 53}", got[0])
	}
}

func TestParsePortMappings_WhitespaceAround(t *testing.T) {
	got := ParsePortMappings("test", " 9000 : 80 ")
	if len(got) != 1 {
		t.Fatalf("expected 1 mapping, got %d", len(got))
	}
	if got[0].ListenPort != 9000 || got[0].TargetPort != 80 {
		t.Errorf("got %+v, want {9000 80}", got[0])
	}
}

func TestParsePortMappings_InvalidTokenSkipped(t *testing.T) {
	got := ParsePortMappings("test", "9000:80,notaport")
	if len(got) != 1 {
		t.Fatalf("expected 1 mapping, got %d", len(got))
	}
	if got[0].ListenPort != 9000 {
		t.Errorf("got %+v, want listen=9000", got[0])
	}
}

func TestParsePortMappings_NonIntegerPortsSkipped(t *testing.T) {
	got := ParsePortMappings("test", "abc:xyz,9000:80")
	if len(got) != 1 {
		t.Fatalf("expected 1 mapping after skipping invalid, got %d", len(got))
	}
	if got[0].ListenPort != 9000 {
		t.Errorf("got %+v, want listen=9000", got[0])
	}
}

func TestParsePortMappings_AllInvalid(t *testing.T) {
	got := ParsePortMappings("test", "abc:xyz,nocolon")
	if len(got) != 0 {
		t.Errorf("expected 0 mappings, got %d", len(got))
	}
}

// ---- ParseIPList ----

func TestParseIPList_PlainIPv4(t *testing.T) {
	nets := ParseIPList("test", "192.168.1.1")
	if len(nets) != 1 {
		t.Fatalf("expected 1 net, got %d", len(nets))
	}
	ip := net.ParseIP("192.168.1.1")
	if !nets[0].Contains(ip) {
		t.Errorf("expected net to contain 192.168.1.1")
	}
	ones, bits := nets[0].Mask.Size()
	if ones != 32 || bits != 32 {
		t.Errorf("expected /32, got /%d", ones)
	}
}

func TestParseIPList_PlainIPv6(t *testing.T) {
	nets := ParseIPList("test", "::1")
	if len(nets) != 1 {
		t.Fatalf("expected 1 net, got %d", len(nets))
	}
	ones, bits := nets[0].Mask.Size()
	if ones != 128 || bits != 128 {
		t.Errorf("expected /128, got /%d", ones)
	}
}

func TestParseIPList_CIDR(t *testing.T) {
	nets := ParseIPList("test", "192.168.0.0/16")
	if len(nets) != 1 {
		t.Fatalf("expected 1 net, got %d", len(nets))
	}
	if !nets[0].Contains(net.ParseIP("192.168.1.100")) {
		t.Errorf("CIDR should contain 192.168.1.100")
	}
	if nets[0].Contains(net.ParseIP("10.0.0.1")) {
		t.Errorf("CIDR should not contain 10.0.0.1")
	}
}

func TestParseIPList_Multiple(t *testing.T) {
	nets := ParseIPList("test", "10.0.0.1,192.168.0.0/16,::1")
	if len(nets) != 3 {
		t.Fatalf("expected 3 nets, got %d", len(nets))
	}
}

func TestParseIPList_InvalidEntrySkipped(t *testing.T) {
	nets := ParseIPList("test", "notanip,10.0.0.1")
	if len(nets) != 1 {
		t.Fatalf("expected 1 net after skipping invalid, got %d", len(nets))
	}
}

func TestParseIPList_Empty(t *testing.T) {
	nets := ParseIPList("test", "")
	if len(nets) != 0 {
		t.Errorf("expected 0 nets for empty string, got %d", len(nets))
	}
}

func TestParseIPList_WhitespaceOnly(t *testing.T) {
	nets := ParseIPList("test", "  ,  ")
	if len(nets) != 0 {
		t.Errorf("expected 0 nets for whitespace-only, got %d", len(nets))
	}
}

// ---- ParseDependants ----

func TestParseDependants_Single(t *testing.T) {
	got := ParseDependants("selenium-chromium")
	if len(got) != 1 || got[0] != "selenium-chromium" {
		t.Errorf("got %v, want [selenium-chromium]", got)
	}
}

func TestParseDependants_Multiple(t *testing.T) {
	got := ParseDependants("a,b,c")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("got %v, want [a b c]", got)
	}
}

func TestParseDependants_WhitespaceTrimmed(t *testing.T) {
	got := ParseDependants(" a , b ")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("got %v, want [a b]", got)
	}
}

func TestParseDependants_Empty(t *testing.T) {
	got := ParseDependants("")
	if len(got) != 0 {
		t.Errorf("got %v, want []", got)
	}
}

func TestParseDependants_BlankTokensSkipped(t *testing.T) {
	got := ParseDependants(",a,")
	if len(got) != 1 || got[0] != "a" {
		t.Errorf("got %v, want [a]", got)
	}
}

// ---- ParseStartTimeoutLabel ----

func TestParseStartTimeoutLabel_ValidPositive(t *testing.T) {
	got := ParseStartTimeoutLabel("svc", "30")
	if got == nil {
		t.Fatal("expected non-nil result for valid positive value")
	}
	if *got != 30*time.Second {
		t.Errorf("got %s, want 30s", *got)
	}
}

func TestParseStartTimeoutLabel_Zero(t *testing.T) {
	got := ParseStartTimeoutLabel("svc", "0")
	if got == nil {
		t.Fatal("expected non-nil result for zero")
	}
	if *got != 0 {
		t.Errorf("got %s, want 0s", *got)
	}
}

func TestParseStartTimeoutLabel_WhitespaceAround(t *testing.T) {
	got := ParseStartTimeoutLabel("svc", "  60  ")
	if got == nil {
		t.Fatal("expected non-nil result; whitespace should be trimmed")
	}
	if *got != 60*time.Second {
		t.Errorf("got %s, want 60s", *got)
	}
}

func TestParseStartTimeoutLabel_Empty(t *testing.T) {
	if got := ParseStartTimeoutLabel("svc", ""); got != nil {
		t.Errorf("expected nil for empty string, got %s", *got)
	}
}

func TestParseStartTimeoutLabel_WhitespaceOnly(t *testing.T) {
	if got := ParseStartTimeoutLabel("svc", "   "); got != nil {
		t.Errorf("expected nil for whitespace-only, got %s", *got)
	}
}

func TestParseStartTimeoutLabel_Negative(t *testing.T) {
	if got := ParseStartTimeoutLabel("svc", "-5"); got != nil {
		t.Errorf("expected nil for negative value, got %s", *got)
	}
}

func TestParseStartTimeoutLabel_NonNumeric(t *testing.T) {
	if got := ParseStartTimeoutLabel("svc", "abc"); got != nil {
		t.Errorf("expected nil for non-numeric value, got %s", *got)
	}
}

// ---- ParseIdleTimeoutLabel ----

func TestParseIdleTimeoutLabel_ValidPositive(t *testing.T) {
	got := ParseIdleTimeoutLabel("svc", "30")
	if got == nil {
		t.Fatal("expected non-nil result for valid positive value")
	}
	if *got != 30*time.Second {
		t.Errorf("got %s, want 30s", *got)
	}
}

func TestParseIdleTimeoutLabel_Zero(t *testing.T) {
	got := ParseIdleTimeoutLabel("svc", "0")
	if got == nil {
		t.Fatal("expected non-nil result for zero (immediate shutdown)")
	}
	if *got != 0 {
		t.Errorf("got %s, want 0s", *got)
	}
}

func TestParseIdleTimeoutLabel_WhitespaceAround(t *testing.T) {
	got := ParseIdleTimeoutLabel("svc", "  60  ")
	if got == nil {
		t.Fatal("expected non-nil result; whitespace should be trimmed")
	}
	if *got != 60*time.Second {
		t.Errorf("got %s, want 60s", *got)
	}
}

func TestParseIdleTimeoutLabel_Empty(t *testing.T) {
	if got := ParseIdleTimeoutLabel("svc", ""); got != nil {
		t.Errorf("expected nil for empty string, got %s", *got)
	}
}

func TestParseIdleTimeoutLabel_WhitespaceOnly(t *testing.T) {
	if got := ParseIdleTimeoutLabel("svc", "   "); got != nil {
		t.Errorf("expected nil for whitespace-only, got %s", *got)
	}
}

func TestParseIdleTimeoutLabel_Negative(t *testing.T) {
	if got := ParseIdleTimeoutLabel("svc", "-5"); got != nil {
		t.Errorf("expected nil for negative value, got %s", *got)
	}
}

func TestParseIdleTimeoutLabel_NonNumeric(t *testing.T) {
	if got := ParseIdleTimeoutLabel("svc", "abc"); got != nil {
		t.Errorf("expected nil for non-numeric value, got %s", *got)
	}
}
