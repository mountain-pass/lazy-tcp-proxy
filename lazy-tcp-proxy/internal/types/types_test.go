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

// ---- ParseHTTPHealthCheckLabel ----

func TestParseHTTPHealthCheckLabel_ValidURL(t *testing.T) {
	got := ParseHTTPHealthCheckLabel("myapp", "http://myapp:8080/health")
	if got != "http://myapp:8080/health" {
		t.Errorf("got %q, want %q", got, "http://myapp:8080/health")
	}
}

func TestParseHTTPHealthCheckLabel_Placeholder(t *testing.T) {
	// {{container}} is NOT substituted at parse time — the raw template is returned
	// so the proxy can resolve the upstream host (IP or DNS) at connection time.
	got := ParseHTTPHealthCheckLabel("myapp", "http://{{container}}:8080/health")
	if got != "http://{{container}}:8080/health" {
		t.Errorf("got %q, want raw template %q", got, "http://{{container}}:8080/health")
	}
}

func TestParseHTTPHealthCheckLabel_PlaceholderHTTPS(t *testing.T) {
	got := ParseHTTPHealthCheckLabel("svc", "https://{{container}}:443/ready")
	if got != "https://{{container}}:443/ready" {
		t.Errorf("got %q, want raw template %q", got, "https://{{container}}:443/ready")
	}
}

func TestParseHTTPHealthCheckLabel_MisspelledPlaceholder(t *testing.T) {
	// A ${...} that isn't {{container}} should be logged as a warning but the URL
	// is still accepted if structurally valid (after treating the bad placeholder
	// as a literal string that happens to be a valid hostname — it won't be, so
	// the URL will be rejected and "" returned).
	got := ParseHTTPHealthCheckLabel("myapp", "http://${Container}:8080/health")
	// ${Container} is not a valid hostname char sequence, so the URL is invalid.
	if got != "" {
		t.Errorf("got %q, want empty string for URL with invalid placeholder host", got)
	}
}

func TestParseHTTPHealthCheckLabel_Empty(t *testing.T) {
	if got := ParseHTTPHealthCheckLabel("myapp", ""); got != "" {
		t.Errorf("expected empty string for empty label, got %q", got)
	}
}

func TestParseHTTPHealthCheckLabel_WhitespaceOnly(t *testing.T) {
	if got := ParseHTTPHealthCheckLabel("myapp", "   "); got != "" {
		t.Errorf("expected empty string for whitespace-only label, got %q", got)
	}
}

func TestParseHTTPHealthCheckLabel_InvalidURL(t *testing.T) {
	if got := ParseHTTPHealthCheckLabel("myapp", "not-a-url"); got != "" {
		t.Errorf("expected empty string for invalid URL, got %q", got)
	}
}

// ---- ParseAvailabilityLabel ----

func TestParseAvailabilityLabel_ValidValues(t *testing.T) {
	cases := []string{"", "ondemand", "cron", "manual"}
	for _, v := range cases {
		if got := ParseAvailabilityLabel("svc", v); got != v {
			t.Errorf("ParseAvailabilityLabel(%q): got %q, want %q", v, got, v)
		}
	}
}

func TestParseAvailabilityLabel_InvalidValue(t *testing.T) {
	if got := ParseAvailabilityLabel("svc", "always"); got != "" {
		t.Errorf("expected empty string for invalid value, got %q", got)
	}
}

func TestParseAvailabilityLabel_Whitespace(t *testing.T) {
	if got := ParseAvailabilityLabel("svc", "  manual  "); got != "manual" {
		t.Errorf("expected %q, got %q", "manual", got)
	}
}

// ---- EffectiveAvailability ----

func TestEffectiveAvailability_ExplicitOnDemand(t *testing.T) {
	info := TargetInfo{Availability: AvailabilityOnDemand}
	if got := EffectiveAvailability(info); got != AvailabilityOnDemand {
		t.Errorf("got %q, want %q", got, AvailabilityOnDemand)
	}
}

func TestEffectiveAvailability_ExplicitCron(t *testing.T) {
	info := TargetInfo{Availability: AvailabilityCron}
	if got := EffectiveAvailability(info); got != AvailabilityCron {
		t.Errorf("got %q, want %q", got, AvailabilityCron)
	}
}

func TestEffectiveAvailability_ExplicitManual(t *testing.T) {
	info := TargetInfo{Availability: AvailabilityManual}
	if got := EffectiveAvailability(info); got != AvailabilityManual {
		t.Errorf("got %q, want %q", got, AvailabilityManual)
	}
}

func TestEffectiveAvailability_DerivedCronFromCronStart(t *testing.T) {
	info := TargetInfo{CronStart: "0 9 * * 1-5"}
	if got := EffectiveAvailability(info); got != AvailabilityCron {
		t.Errorf("got %q, want %q", got, AvailabilityCron)
	}
}

func TestEffectiveAvailability_DerivedCronFromCronStop(t *testing.T) {
	info := TargetInfo{CronStop: "0 17 * * 1-5"}
	if got := EffectiveAvailability(info); got != AvailabilityCron {
		t.Errorf("got %q, want %q", got, AvailabilityCron)
	}
}

func TestEffectiveAvailability_DerivedOnDemandWhenNoCron(t *testing.T) {
	info := TargetInfo{}
	if got := EffectiveAvailability(info); got != AvailabilityOnDemand {
		t.Errorf("got %q, want %q", got, AvailabilityOnDemand)
	}
}

func TestEffectiveAvailability_ExplicitOnDemandOverridesCronExpressions(t *testing.T) {
	info := TargetInfo{Availability: AvailabilityOnDemand, CronStart: "0 9 * * 1-5"}
	if got := EffectiveAvailability(info); got != AvailabilityOnDemand {
		t.Errorf("explicit ondemand should override cron expressions; got %q", got)
	}
}

// ---- ParseTraefikHostSpecs ----

func TestParseTraefikHostSpecs_Valid(t *testing.T) {
	got := ParseTraefikHostSpecs("test", "s3.example.com:9000,mongo.example.com:27017")
	if len(got) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(got))
	}
	if got[0].Domain != "s3.example.com" || got[0].TargetPort != 9000 {
		t.Errorf("spec 0: got %+v", got[0])
	}
	if got[1].Domain != "mongo.example.com" || got[1].TargetPort != 27017 {
		t.Errorf("spec 1: got %+v", got[1])
	}
}

func TestParseTraefikHostSpecs_InvalidPort(t *testing.T) {
	got := ParseTraefikHostSpecs("test", "bad.host:notanumber")
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %+v", got)
	}
}

func TestParseTraefikHostSpecs_EmptyDomain(t *testing.T) {
	got := ParseTraefikHostSpecs("test", ":8000")
	if len(got) != 0 {
		t.Errorf("expected empty slice for blank domain, got %+v", got)
	}
}

func TestParseTraefikHostSpecs_MixedEmptyDomain(t *testing.T) {
	got := ParseTraefikHostSpecs("test", "good.com:9000,:8000")
	if len(got) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(got))
	}
	if got[0].Domain != "good.com" || got[0].TargetPort != 9000 {
		t.Errorf("spec 0: got %+v", got[0])
	}
}

func TestParseTraefikHostSpecs_MissingColon(t *testing.T) {
	got := ParseTraefikHostSpecs("test", "nodomain")
	if len(got) != 0 {
		t.Errorf("expected empty slice for missing colon, got %+v", got)
	}
}

func TestParseTraefikHostSpecs_WhitespaceAround(t *testing.T) {
	got := ParseTraefikHostSpecs("test", "  myapp.local : 8080 ")
	if len(got) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(got))
	}
	if got[0].Domain != "myapp.local" || got[0].TargetPort != 8080 {
		t.Errorf("spec 0: got %+v", got[0])
	}
}
