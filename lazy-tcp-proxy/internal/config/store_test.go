package config

import (
	"reflect"
	"testing"
)

func TestEntryToTargetInfo_TLSOptions(t *testing.T) {
	entry := ServiceEntry{
		Name:          "pg",
		Ports:         []string{"5432:5432"},
		ALPNProtocols: []string{"postgresql, h2", "postgresql"},
		MinVersion:    "1.3",
	}
	info, err := entryToTargetInfo(entry, nil)
	if err != nil {
		t.Fatalf("entryToTargetInfo: %v", err)
	}
	if want := []string{"postgresql", "h2"}; !reflect.DeepEqual(info.ALPNProtocols, want) {
		t.Errorf("ALPNProtocols: got %v, want %v", info.ALPNProtocols, want)
	}
	if info.MinTLSVersion != "1.3" {
		t.Errorf("MinTLSVersion: got %q, want 1.3", info.MinTLSVersion)
	}
}

func TestEntryToTargetInfo_InvalidMinVersionIgnored(t *testing.T) {
	entry := ServiceEntry{Name: "pg", Ports: []string{"5432:5432"}, MinVersion: "tls13"}
	info, err := entryToTargetInfo(entry, nil)
	if err != nil {
		t.Fatalf("entryToTargetInfo: %v", err)
	}
	if info.MinTLSVersion != "" {
		t.Errorf("MinTLSVersion: got %q, want empty", info.MinTLSVersion)
	}
}
