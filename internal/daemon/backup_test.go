package daemon

import (
	"context"
	"encoding/json"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/link"
)

const restoreLAN = `"link":{"adopted":["lan0"],"networks":[{"name":"home","members":["lan0"],"ipv4":{"subnet":"192.168.50.0/24","router":"192.168.50.1"}}]}`

func TestBackupValidationUsesCandidateNetworks(t *testing.T) {
	s := configurationStore(filepath.Join(t.TempDir(), "olr.json"))
	body := `{"$schema":"` + core.SchemaURL + `",` + restoreLAN + `,"dhcp":{"enabled":true,"pools":[{"network":"home","ipv4":{"start":"192.168.50.20","end":"192.168.50.100"}}]}}`
	source := func() ([]link.Interface, error) {
		return []link.Interface{{Name: "lan0", Up: true, Prefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.1/24")}}}, nil
	}
	doc, err := s.ParseBackup([]byte(body), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateBackup(context.Background(), doc, source); err != nil {
		t.Fatal("valid candidate rejected", err)
	}
	for _, bad := range []string{
		strings.Replace(body, `"network":"home"`, `"network":"missing"`, 1),
		strings.ReplaceAll(body, `lan0`, `missing0`),
		strings.Replace(body, `"enabled":true`, `"enabled":true,"typo":true`, 1),
		strings.Replace(body, `"$schema"`, `"system":{"access":{"password":{"hash":"broken"}}},"$schema"`, 1),
	} {
		doc, err := s.ParseBackup([]byte(bad), nil)
		if err == nil {
			err = validateBackup(context.Background(), doc, source)
		}
		if err == nil {
			t.Fatalf("invalid restore accepted: %s", bad)
		}
	}
}

func TestBackupValidationAcceptsEmptyConfiguration(t *testing.T) {
	s := configurationStore(filepath.Join(t.TempDir(), "olr.json"))
	doc, err := s.ParseBackup([]byte(`{"$schema":"`+core.SchemaURL+`"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateBackup(context.Background(), doc, func() ([]link.Interface, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
}

func TestConfigurationSchemaKeepsModuleTypesDistinct(t *testing.T) {
	p, err := core.Reflect("configuration", configurationSchema{})
	if err != nil {
		t.Fatal(err)
	}
	linkSchema, ok := p.Full.Properties.Get("link")
	if !ok {
		t.Fatal("missing link schema")
	}
	systemSchema, ok := p.Full.Properties.Get("system")
	if !ok {
		t.Fatal("missing system schema")
	}
	l, _ := json.Marshal(linkSchema)
	s, _ := json.Marshal(systemSchema)
	if string(l) == string(s) || !strings.Contains(string(l), "internal_link_Config") || !strings.Contains(string(s), "internal_system_Config") {
		t.Fatalf("module Config definitions collided: %s %s", l, s)
	}
}
