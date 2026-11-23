package api

import (
	"strings"
	"testing"
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
