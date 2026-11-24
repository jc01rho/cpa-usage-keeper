package api

import (
	"strings"
	"testing"

	"cpa-usage-keeper/internal/entities"
)

func TestAnalysisAPIKeyIdentityForInstanceMasksUnregisteredKeys(t *testing.T) {
	const instanceID = "instance-a"
	const registered = "registered-key-0123456789"
	const unregistered = "unregistered-key-0123456789"
	infos := map[string]analysisAPIKeyInfo{
		apiKeyInfoLookupKey(instanceID, registered): {ID: "7", InstanceID: instanceID, Label: "Registered"},
	}

	key, label := analysisAPIKeyIdentityForInstance(instanceID, registered, infos)
	if key != "7" || label != "Registered" {
		t.Fatalf("registered key identity = %q, %q", key, label)
	}

	key, label = analysisAPIKeyIdentityForInstance(instanceID, unregistered, infos)
	if !strings.HasPrefix(key, "legacy:") || strings.Contains(key, unregistered) || label != "sk-*********456789" {
		t.Fatalf("unregistered key identity must be masked, got %q, %q", key, label)
	}

	otherKey, _ := analysisAPIKeyIdentityForInstance(instanceID, unregistered+"x", infos)
	if otherKey == key {
		t.Fatalf("distinct unregistered keys must not share a response key: %q", key)
	}
}

func TestAnalysisAPIKeyIdentityForInstanceResolvesToMainRegistration(t *testing.T) {
	const apiKey = "shared-key-0123456789"
	infos := map[string]analysisAPIKeyInfo{
		apiKeyInfoLookupKey(entities.LegacyCPAInstanceID, apiKey): {ID: "59", InstanceID: entities.LegacyCPAInstanceID, Label: "Main"},
		apiKeyInfoLookupKey("instance-b", apiKey):                 {ID: "70", InstanceID: "instance-b", Label: "Own"},
	}

	key, label := analysisAPIKeyIdentityForInstance("instance-a", apiKey, infos)
	if key != "59" || label != "Main" {
		t.Fatalf("a key unregistered on its instance must resolve to the main registration, got %q, %q", key, label)
	}

	key, label = analysisAPIKeyIdentityForInstance("instance-b", apiKey, infos)
	if key != "70" || label != "Own" {
		t.Fatalf("a key registered on its own instance must keep that registration, got %q, %q", key, label)
	}
}
